/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// The expected gateways are the readiness gate of the gw-admin pods: a pod
// only turns Ready, and joins the pods the data planes reach, once told the
// gateways it serves, so that a pod of a rollout, or of a scale up, never
// serves a data plane before it holds the configuration. Once Ready, it stays
// so. The reconcile of a gateway provisions the pods of its GWAdmin, and the
// removal of its last gateway tells the pods serving they serve none, but a
// GWAdmin no OnPremEventGateway may use has no reconcile to provision its new
// pods: they would never turn Ready, and every rollout of the GWAdmin, e.g.
// one of a rotation of its certificate authorities, would stall. The
// controller set up by setupGWAdminsWithoutGateways tells them they serve no
// gateway.
//
// It reads the gateways from the cache: a gateway just created may not be in
// it yet, and a starting pod may then be told it serves none, and turn Ready.
// That gateway has no data plane yet, and its reconcile, waiting in line for
// the lock, pushes it to every pod right after.

// setupGWAdminsWithoutGateways sets up the controller provisioning the
// gw-admin pods of the GWAdmins no OnPremEventGateway may use. A change to a
// GWAdmin, to its Secrets or to its gw-admin pods triggers it, and so do the
// deletion of a gateway and a change to a KongReferenceGrant of the
// namespace of a GWAdmin, which may leave it without gateways. It shares the
// Syncer of the gateways, and its lock: the pods are told one set of
// gateways at a time. Its reconciles, like those of the gateways, run
// several at once, so that a gw-admin pod that doesn't answer delays its
// GWAdmin only.
func (r *Reconciler) setupGWAdminsWithoutGateways(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		Named(gwAdminControllerName).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles})
	b = r.GWAdminSync.WatchGWAdmins(b, func(_ context.Context, gwAdmin client.ObjectKey) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: gwAdmin}}
	})
	b = b.Watches(&eventgatewayv1alpha1.OnPremEventGateway{},
		handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			gateway, ok := obj.(*eventgatewayv1alpha1.OnPremEventGateway)
			if !ok {
				return nil
			}
			return []reconcile.Request{{NamespacedName: gateway.GWAdminKey()}}
		}),
		builder.WithPredicates(onlyDeletions()))
	b = b.Watches(&configurationv1alpha1.KongReferenceGrant{}, handler.EnqueueRequestsFromMapFunc(mapKongReferenceGrantToGWAdmins(mgr.GetClient())))
	if err := b.Complete(reconcile.Func(r.provisionGWAdminWithoutGateways)); err != nil {
		return fmt.Errorf("failed to set up the %s controller: %w", gwAdminControllerName, err)
	}
	return nil
}

// mapKongReferenceGrantToGWAdmins returns a MapFunc that enqueues the
// GWAdmins of the namespace of a KongReferenceGrant: it may allow, or no
// longer allow, the gateways of another namespace to use them.
func mapKongReferenceGrantToGWAdmins(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var gwAdmins operatorv1alpha1.GWAdminList
		if err := cl.List(ctx, &gwAdmins, client.InNamespace(obj.GetNamespace())); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to list the GWAdmins of a namespace", "namespace", obj.GetNamespace())
			return nil
		}
		requests := make([]reconcile.Request, 0, len(gwAdmins.Items))
		for i := range gwAdmins.Items {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gwAdmins.Items[i])})
		}
		return requests
	}
}

// provisionGWAdminWithoutGateways tells the gw-admin pods of the GWAdmin not
// serving yet, e.g. new ones, or restarted ones, as gw-admin keeps nothing
// across a restart, that they serve no gateway, when no OnPremEventGateway
// may use it: when one may, its reconcile provisions them. A pod serving
// passed the readiness gate already.
func (r *Reconciler) provisionGWAdminWithoutGateways(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, gwAdminControllerName, r.LoggingMode)
	key := gwAdminBackoffKey(req.NamespacedName)

	// Most GWAdmins have gateways: they are left to their reconciles without
	// taking the lock they wait for. The gateways are read again under the
	// lock, which the push of a set of gateways must hold.
	if served, err := gatewaysServedBy(ctx, r.Client, req.NamespacedName); err != nil || len(served) > 0 {
		r.forgetGWAdmin(key)
		return ctrl.Result{}, err
	}
	unlock, locked := r.GWAdminSync.TryLockGWAdmin(req.NamespacedName)
	if !locked {
		// The unlock wakes it: the requeue is in case that is lost.
		return ctrl.Result{RequeueAfter: lockRetryInterval}, nil
	}
	defer unlock()

	served, err := gatewaysServedBy(ctx, r.Client, req.NamespacedName)
	if err != nil || len(served) > 0 {
		r.forgetGWAdmin(key)
		return ctrl.Result{}, err
	}
	target, err := r.GWAdminSync.Resolve(ctx, req.NamespacedName, gwadminsync.ForPush)
	if _, notReady := errors.AsType[gwadminsync.NotReadyError](err); notReady {
		// A change to the GWAdmin, e.g. once it publishes its API, triggers
		// it again. Its pods, once there, are tried right away.
		r.forgetGWAdmin(key)
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	starting := slices.DeleteFunc(slices.Clone(target.Pods), func(pod gwadminsync.Pod) bool { return pod.Serving })
	if len(starting) == 0 {
		r.forgetGWAdmin(key)
		return ctrl.Result{}, nil
	}

	// A push that failed is tried again after its backoff, like those of the
	// gateways, whatever triggers it, unless the pods told change, e.g. one
	// failing is replaced.
	inputs := targetInputs(starting, target)
	if wait, _ := r.backoff.Wait(key, inputs, time.Now()); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}
	pending, err := r.GWAdminSync.ForEachPod(ctx, target, starting,
		func(ctx context.Context, pod gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface) error {
			return r.setExpected(ctx, target, pod, gwAdmin, []uuid.UUID{})
		})
	if err != nil {
		if ctx.Err() != nil {
			// e.g. the operator is stopping: nothing failed.
			return ctrl.Result{}, ctx.Err()
		}
		// No gateway reports it: the GWAdmin does, once, not on every retry.
		reported := r.backoff.Failing(key, inputs)
		retryAfter := r.backoff.Failed(key, inputs, err, time.Now())
		if !reported {
			r.gwAdminEvent(target.GWAdmin, corev1.EventTypeWarning, eventReasonProvisionFailed, eventActionProvision,
				"The gw-admin pods can't be told they serve no OnPremEventGateway: "+err.Error())
		}
		log.Error(logger, err, "failed to tell the gw-admin pods of a GWAdmin without gateways they serve none, trying again later",
			"gwAdmin", req.NamespacedName, "retryAfter", retryAfter.String())
		return ctrl.Result{RequeueAfter: retryAfter}, nil
	}
	r.backoff.Forget(key)
	if len(pending) == 0 {
		r.pendingRetries().Forget(key)
		return ctrl.Result{}, nil
	}
	// e.g. crash looping: tried again less and less often, and right away once
	// its pods change.
	retryAfter := r.pendingRetries().Failed(key, inputs, nil, time.Now())
	log.Debug(logger, "gw-admin pods of a GWAdmin without gateways are still pending",
		"gwAdmin", req.NamespacedName, "pending", len(pending), "retryAfter", retryAfter.String())
	return ctrl.Result{RequeueAfter: retryAfter}, nil
}

// gwAdminBackoffKey keys the GWAdmin in the backoffs of the pushes, shared
// with the gateways, keyed by their UIDs: its key isn't a UID, so that it is
// forgotten even once the GWAdmin is gone.
func gwAdminBackoffKey(gwAdmin types.NamespacedName) types.UID {
	return types.UID("GWAdmin " + gwAdmin.String())
}

// forgetGWAdmin forgets the failures of the pushes to the GWAdmin, e.g. once
// a gateway may use it, or it is gone.
func (r *Reconciler) forgetGWAdmin(key types.UID) {
	r.backoff.Forget(key)
	r.pendingRetries().Forget(key)
}

// gwAdminEvent records an event of the GWAdmin.
func (r *Reconciler) gwAdminEvent(gwAdmin *operatorv1alpha1.GWAdmin, eventType, reason, action, note string) {
	if r.eventRecorder != nil {
		r.eventRecorder.Eventf(gwAdmin, nil, eventType, reason, action, "%s", k8sutils.TruncateMessage(note, maxEventNoteLength))
	}
}
