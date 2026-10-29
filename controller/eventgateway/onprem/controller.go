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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// Reconciler reconciles an OnPremEventGateway object. One reconcile serves
// one gateway: a change to the gateway, to any Event Gateway entity that
// belongs to it, directly or through its parent entities, to its GWAdmin, the
// Secrets of the GWAdmin or its gw-admin pods, or to a KongReferenceGrant
// allowing it to use a GWAdmin in another namespace, or its entities to read
// Secrets of another namespace, or to a Secret its entities read, triggers
// it.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	// GWAdminSync reaches the gw-admin pods of the GWAdmins. It is the
	// reconciler's own, shared by its two controllers, of the gateways and of
	// the GWAdmins without gateways: what it keeps, e.g. what was written to
	// each pod, is of the gateways only.
	GWAdminSync *gwadminsync.Syncer

	// TypeConverter converts the gateways for the Server-Side Apply of their
	// status.
	TypeConverter managedfields.TypeConverter

	// eventRecorder records the events of the gateways. SetupWithManager sets
	// it; when nil, no event is recorded.
	eventRecorder events.EventRecorder

	// backoff delays the pushes and the removals that failed.
	backoff gwadminsync.Backoff
	// terminatingTimeout overrides terminatingPodTimeout, in tests.
	terminatingTimeout time.Duration
	// pending spaces the requeues of a gateway whose gw-admin pods are still
	// pending (see gwadminsync.Syncer.ForEachPod), e.g. one crash looping is
	// pending for long: a reconcile triggered otherwise pushes to them right
	// away.
	pending     *gwadminsync.Backoff
	pendingOnce sync.Once
	// removedFromGWAdmin holds the UIDs of the gateways that may not use
	// their GWAdmin and that every gw-admin pod was told to stop serving:
	// they aren't told again on every change of the GWAdmin.
	removedFromGWAdmin sync.Map
	// lastEntities holds, by gateway UID, the entities of the last
	// configuration built for the gateway, by the UID of their object (see
	// buildConfiguration).
	//
	// It is kept in memory: after a restart of the operator, an entity whose
	// Secret is gone is left out.
	lastEntities sync.Map
	// reported holds, by gateway UID, the objects whose status was last
	// reported for the gateway (see reportDetached). Kept in memory too.
	reported sync.Map
}

// pendingRetries returns the backoff of the pushes to the gw-admin pods still
// pending.
func (r *Reconciler) pendingRetries() *gwadminsync.Backoff {
	r.pendingOnce.Do(func() { r.pending = &gwadminsync.Backoff{Base: pendingRetryBase, Max: pendingRetryMax} })
	return r.pending
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)

	b := ctrl.NewControllerManagedBy(mgr).
		// Every change to the gateway triggers it, its status too, so a
		// status changed by someone else is set back. A push only writes to
		// gw-admin what changed, and a push that failed waits for its backoff,
		// so the status writes of the reconcile cost two reads per gw-admin pod,
		// its entities and its data plane certificates, one per terminating
		// pod, and no write.
		For(&eventgatewayv1alpha1.OnPremEventGateway{}, builder.WithPredicates(r.forgetDeleted())).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrentReconciles})

	// An update is mapped for both the old and the new object, so an entity
	// whose parent reference changes, which the CRDs only allow while it is not
	// Programmed, triggers both gateways. A delete is mapped for the deleted
	// object. GenerationChangedPredicate ignores the status writes on the
	// entities, e.g. the Programmed condition the gateway reconcile will set.
	mapToGateway := handler.EnqueueRequestsFromMapFunc(mapEntityToOnPremEventGateway(mgr.GetClient()))
	for _, entity := range entityTypes() {
		b = b.Watches(entity, mapToGateway, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	}

	// A change to the GWAdmin of a gateway, to its Secrets or to its gw-admin
	// pods triggers it, and so does a change to a KongReferenceGrant allowing
	// the gateways of a namespace to use a GWAdmin of another, or their
	// entities to read the Secrets of another.
	requests := func(ctx context.Context, gwAdmin types.NamespacedName) []reconcile.Request {
		return requestsForGWAdmin(ctx, mgr.GetClient(), gwAdmin)
	}
	b = r.GWAdminSync.Watch(b, requests)
	b = b.Watches(&configurationv1alpha1.KongReferenceGrant{}, handler.EnqueueRequestsFromMapFunc(mapKongReferenceGrantToOnPremEventGateways(mgr.GetClient())))
	// A change to a Secret an entity reads triggers its gateway.
	b = b.Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapSecretToOnPremEventGateways(mgr.GetClient())))

	// The deletion of a gateway triggers the other gateways of its GWAdmin:
	// they set the gateways gw-admin serves again, without it, e.g. when it
	// is deleted before its first reconcile, which has nothing to clean up.
	b = b.Watches(&eventgatewayv1alpha1.OnPremEventGateway{},
		handler.EnqueueRequestsFromMapFunc(mapGatewayToGatewaysOfItsGWAdmin(mgr.GetClient())),
		builder.WithPredicates(onlyDeletions()))

	if err := b.Complete(reconcile.AsReconciler(r.Client, r)); err != nil {
		return err
	}
	if err := r.setupGWAdminsWithoutGateways(mgr); err != nil {
		return err
	}
	return r.setupUnreachable(mgr)
}

// Reconcile pushes the configuration of an OnPremEventGateway to its
// gw-admin, and makes gw-admin stop serving it when it is deleted.
func (r *Reconciler) Reconcile(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling OnPremEventGateway resource")

	gatewayID, ok := uidV4(gateway)
	if !ok {
		return requeueOnConflict(logger, ctrl.Result{}, r.reportInvalidUID(ctx, logger, gateway))
	}

	// A push sets the whole set of the gateways gw-admin serves
	// (SetExpectedEventGateways), computed from the gateways of the GWAdmin
	// in the cache: two gateways of a GWAdmin reconciled at once could each
	// set a set missing the other, e.g. one just created, and gw-admin would
	// stop serving it. One reconcile at a time per GWAdmin makes the last
	// set the one computed last, from a cache holding every gateway reconciled
	// before. A reconcile that doesn't get the lock tries again later: it
	// doesn't hold a worker the gateways of other GWAdmins wait for.
	unlock, locked := r.GWAdminSync.TryLock(gateway.GWAdminKey(), client.ObjectKeyFromObject(gateway))
	if !locked {
		// The unlock wakes it: the requeue is in case that is lost.
		log.Trace(logger, "another OnPremEventGateway of the GWAdmin is being reconciled, waiting for it")
		return ctrl.Result{RequeueAfter: lockRetryInterval}, nil
	}
	defer unlock()

	if !gateway.DeletionTimestamp.IsZero() {
		res, err := r.cleanup(ctx, logger, gateway, gatewayID)
		if err == nil && res.IsZero() {
			log.Debug(logger, "OnPremEventGateway cleanup completed")
		}
		return requeueOnConflict(logger, res, err)
	}

	if _, res, err := patch.WithFinalizer(ctx, r.Client, gateway, FinalizerGWAdminCleanup); err != nil || !res.IsZero() {
		return res, err
	}

	// The revision of the configuration is recorded before it is pushed.
	previous, _ := r.lastEntities.Load(gateway.UID)
	cfg, err := buildConfiguration(ctx, r.Client, gateway, gwadminref.SecretHint(r.GWAdminSync.SecretLabelSelector), asEntities(previous))
	if err != nil {
		return requeueOnConflict(logger, ctrl.Result{}, err)
	}
	r.lastEntities.Store(gateway.UID, cfg.built)
	config := deriveConfig(gateway, cfg)
	if err := r.recordRevision(ctx, gateway, config); err != nil {
		return requeueOnConflict(logger, ctrl.Result{}, err)
	}
	config.Version = configVersion(gateway)

	result, pushErr, err := r.pushConfiguration(ctx, gateway, gatewayID, config, cfg)
	if !result.done {
		// The push has no outcome yet, e.g. the gateways of the GWAdmin
		// couldn't be listed: it is retried, and the status is left as it is.
		return requeueOnConflict(logger, ctrl.Result{}, err)
	}
	if statusErr := r.reportPush(ctx, logger, gateway, result, pushErr); statusErr != nil {
		err = errors.Join(err, statusErr)
	}
	if statusErr := r.reportEntities(ctx, gateway, cfg, result); statusErr != nil {
		err = errors.Join(err, statusErr)
	}
	switch {
	case err != nil:
	case result.retryAfter > 0:
		// A push, or a removal, that failed is tried again after its backoff,
		// not right away: it isn't returned as an error. One that failed
		// recently waits for it, the status reporting that failure again. The
		// pods still pending are tried again later: no change of their
		// endpoint tells once they listen, as they only turn Ready once
		// provisioned.
		switch {
		case result.failed:
			log.Error(logger, pushErr, "failed to push the OnPremEventGateway to gw-admin, trying again later", "retryAfter", result.retryAfter.String())
		case len(result.pending) > 0:
			log.Debug(logger, "gw-admin pods are not serving yet, pushing to them later", "pods", len(result.pending), "retryAfter", result.retryAfter.String())
		case result.terminatingFailed:
			log.Debug(logger, "terminating gw-admin pods couldn't be reached, trying again later", "retryAfter", result.retryAfter.String())
		default:
			log.Debug(logger, "the push of the OnPremEventGateway failed recently, waiting before trying again", "retryAfter", result.retryAfter.String())
		}
		return ctrl.Result{RequeueAfter: result.retryAfter}, nil
	default:
		// A GWAdmin that isn't ready is not retried: a change to it, to its
		// Secrets or to its pods triggers the gateway again.
		log.Debug(logger, "reconciliation complete for OnPremEventGateway resource")
	}
	return requeueOnConflict(logger, ctrl.Result{}, err)
}

// invalidUIDError tells the UID of a gateway is not a version 4 UUID, which
// gw-admin requires as the id of a gateway. The API server generates version
// 4 UUIDs: such a UID was set by other means, and retrying doesn't change it.
type invalidUIDError struct {
	uid types.UID
}

func (e invalidUIDError) Error() string {
	return fmt.Sprintf("the UID %q of the OnPremEventGateway is not a version 4 UUID, which gw-admin requires as the id of a gateway", e.uid)
}

// reportInvalidUID reports on the gateway, and on its entities, that it can't
// be pushed because of its UID. It is not retried. A gateway being deleted
// has nothing to clean up: it never got its finalizer.
func (r *Reconciler) reportInvalidUID(ctx context.Context, logger logr.Logger, gateway *eventgatewayv1alpha1.OnPremEventGateway) error {
	if !gateway.DeletionTimestamp.IsZero() {
		return nil
	}
	invalid := invalidUIDError{uid: gateway.GetUID()}
	return errors.Join(
		r.reportPush(ctx, logger, gateway, pushResult{done: true}, invalid),
		r.reportEntitiesNotProgrammed(ctx, gateway, invalid.Error()),
	)
}

// reportPush sets the status of the gateway from the outcome of the push, and
// records a Warning event when its configuration stops being pushed.
func (r *Reconciler) reportPush(
	ctx context.Context, logger logr.Logger, gateway *eventgatewayv1alpha1.OnPremEventGateway, result pushResult, pushErr error,
) error {
	pushedType := string(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType)
	wasPushed := apimeta.IsStatusConditionTrue(gateway.Status.Conditions, pushedType)
	if err := r.applyStatus(ctx, logger, gateway, result, pushErr); err != nil {
		return err
	}
	// Only when a pushed gateway stops being pushed, e.g. a pod failed, or
	// every pod is pending: a failure retried with backoff would record an
	// event on every attempt, each with its own message, and a new gateway
	// waiting for its GWAdmin, e.g. one created along with it, is not worth a
	// Warning.
	if pushed := apimeta.FindStatusCondition(gateway.Status.Conditions, pushedType); wasPushed && pushed != nil && pushed.Status != metav1.ConditionTrue {
		r.event(gateway, corev1.EventTypeWarning, pushed.Reason, eventActionPushConfiguration,
			"The configuration can't be pushed to gw-admin: "+pushed.Message)
	}
	return nil
}

// event records an event on the gateway.
func (r *Reconciler) event(gateway *eventgatewayv1alpha1.OnPremEventGateway, eventType, reason, action, note string) {
	if r.eventRecorder != nil {
		r.eventRecorder.Eventf(gateway, nil, eventType, reason, action, "%s", k8sutils.TruncateMessage(note, maxEventNoteLength))
	}
}

// requeueOnConflict requeues right away when the reconcile failed on a
// conflict, e.g. an optimistic lock on an object changed since it was read:
// the next reconcile reads it again.
func requeueOnConflict(logger logr.Logger, res ctrl.Result, err error) (ctrl.Result, error) {
	if err != nil && apierrors.IsConflict(err) {
		log.Debug(logger, "conflict while reconciling, requeueing", "reason", err.Error())
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	return res, err
}

// pushResult is what a push of the configuration of a gateway got to.
type pushResult struct {
	// done tells the push has an outcome the status of the gateway reports:
	// the configuration was pushed, a gw-admin pod failed, or the GWAdmin
	// can't be pushed to. It is false when the push didn't get that far, e.g.
	// the gateways of the GWAdmin couldn't be listed.
	done bool
	// target is the gw-admin of the gateway, when it could be resolved.
	target *gwadminsync.Target
	// pending are the gw-admin pods not pushed to because they are still
	// pending, e.g. starting: they are pushed to later.
	pending []gwadminsync.Pod
	// retryAfter, when set, tells when to try again: after a push, or a
	// removal, that failed, either now (failed) or recently, its error then
	// being the one of the last that failed, or when pods are pending, or
	// terminating pods couldn't be reached.
	retryAfter time.Duration
	// terminatingFailed tells a terminating gw-admin pod couldn't be told to
	// stop trusting the data plane certificates the configuration no longer
	// holds: it is tried again like a pod pending.
	terminatingFailed bool
	// failed tells the push, or the removal, failed now.
	failed bool
	// certificates tells, once pushed, which gw-admin pods hold each data
	// plane certificate of the configuration, and which rejected it. It is
	// nil when nothing was pushed, e.g. while the push waits for its backoff.
	certificates *certificateOutcome
}

// pushConfiguration pushes the configuration of the gateway to every
// gw-admin pod. It returns what it got to, even when the push failed, and
// the error of the push, which the status of the gateway reports. Only the
// outcome of the calls to gw-admin and the readiness of the GWAdmin are
// errors of the push: any other error, e.g. listing the gateways of the
// GWAdmin, is returned apart, and only retried.
func (r *Reconciler) pushConfiguration(
	ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID, config gatewayConfig, cfg configuration,
) (result pushResult, pushErr error, err error) {
	target, err := r.resolveGWAdmin(ctx, gateway, true)
	if notReady, ok := errors.AsType[gwAdminNotReadyError](err); ok {
		return r.handleUnavailableGWAdmin(ctx, gateway, gatewayID, notReady)
	}
	if err != nil {
		return result, nil, err
	}
	expected, err := expectedGateways(ctx, r.Client, gateway)
	if err != nil {
		return result, nil, err
	}
	// The gateway may use its GWAdmin again: if it may not anymore later,
	// gw-admin is told to stop serving it.
	r.removedFromGWAdmin.Delete(gateway.UID)

	inputs := pushInputs(target, gateway, expected)
	if wait, lastErr := r.backoff.Wait(gateway.UID, inputs, time.Now()); wait > 0 {
		return pushResult{done: true, target: target, retryAfter: wait}, lastErr, nil
	}
	// The terminating pods aren't pushed to, but may still serve data planes:
	// they stop trusting the data plane certificates the configuration no
	// longer holds as they are, as far as they can be reached within
	// terminatingPodTimeout, not to hold the lock of the GWAdmin. One that
	// can't be is tried again like a pod pending.
	certificates := newCertificateOutcome()
	var terminatingFailed atomic.Bool
	pending, pushErr := r.GWAdminSync.ForEachPod(ctx, target, slices.Concat(target.Pods, target.Terminating),
		func(ctx context.Context, pod gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface) error {
			if slices.Contains(target.Terminating, pod) {
				ctx, cancel := context.WithTimeout(ctx, cmp.Or(r.terminatingTimeout, terminatingPodTimeout))
				defer cancel()
				if err := removeUnconfigured(ctx, gwAdmin, gatewayID, cfg); err != nil {
					terminatingFailed.Store(true)
					log.Debug(log.GetLogger(ctx, ControllerName, r.LoggingMode),
						"a terminating gw-admin pod can't be told to stop trusting data plane certificates", "pod", pod.Name, "reason", err.Error())
				}
				return nil
			}
			err := r.push(ctx, target, pod, gwAdmin, config, cfg, gatewayID, expected, certificates)
			if err != nil {
				certificates.fail(pod)
			}
			return err
		})
	result = pushResult{done: true, target: target, pending: pending, certificates: certificates, terminatingFailed: terminatingFailed.Load()}
	if pushErr != nil {
		result.retryAfter, result.failed = r.backoff.Failed(gateway.UID, inputs, pushErr, time.Now()), true
		return result, pushErr, nil
	}
	r.backoff.Forget(gateway.UID)
	if len(pending) == 0 && !result.terminatingFailed {
		r.pendingRetries().Forget(gateway.UID)
		return result, nil, nil
	}
	result.retryAfter = r.pendingRetries().Failed(gateway.UID, inputs, nil, time.Now())
	return result, nil, nil
}

// pushInputs tells what a push of the gateway is made of: its gw-admin pods,
// whether each is serving, the TLS material they are reached with, the
// revision of its configuration and the expected gateways. A push that failed
// is tried again right away once they change, e.g. once a pod starts or stops
// serving, or the configuration changes; a change that leaves them as they
// are, e.g. of the status of the gateway, waits for the backoff.
func pushInputs(target *gwadminsync.Target, gateway *eventgatewayv1alpha1.OnPremEventGateway, expected []uuid.UUID) string {
	return strings.Join([]string{targetInputs(target.Pods, target), strconv.FormatInt(gateway.Status.ConfigRevision, 10), idsKey(expected)}, " ")
}

// targetInputs tells what the pods of the target are: which, where, whether
// each is serving, and the TLS material they are reached with.
func targetInputs(pods []gwadminsync.Pod, target *gwadminsync.Target) string {
	keys := make([]string, 0, len(pods))
	for _, pod := range pods {
		keys = append(keys, pod.Key()+"="+pod.Address+"/"+strconv.FormatBool(pod.Serving))
	}
	return target.TLSFingerprint + " " + strings.Join(keys, ",")
}

// handleUnavailableGWAdmin makes gw-admin stop serving the gateway when it may
// not use its GWAdmin anymore. It returns what the push got to, its error,
// and apart any other error, which is only retried.
func (r *Reconciler) handleUnavailableGWAdmin(
	ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID, notReady gwAdminNotReadyError,
) (result pushResult, pushErr error, err error) {
	result = pushResult{done: true}
	if !notReady.notAllowed {
		return result, notReady, nil
	}
	// The gateway may not use the GWAdmin, e.g. its KongReferenceGrant was
	// removed: the gw-admin pods stop serving it, once. A new pod doesn't
	// serve it either: gw-admin keeps nothing across a restart, and the
	// gateways of the GWAdmin no longer push it.
	if _, removed := r.removedFromGWAdmin.Load(gateway.UID); removed {
		return result, notReady, nil
	}
	target, err := r.removalTarget(ctx, gateway)
	if removeNotReady, ok := errors.AsType[gwAdminNotReadyError](err); ok {
		// Not retried: a change to the GWAdmin, e.g. once its TLS material
		// is back, triggers the gateway again.
		return result, errors.Join(notReady, fmt.Errorf("gw-admin can't be told to stop serving the gateway yet: %w", removeNotReady)), nil
	}
	if err != nil {
		return result, notReady, err
	}
	if target != nil {
		// Its inputs are those of the pods it tells: once they change, e.g. a
		// pod failing is replaced, a removal that failed is tried again.
		inputs := "removal " + targetInputs(slices.Concat(target.Pods, target.Terminating), target)
		if wait, lastErr := r.backoff.Wait(gateway.UID, inputs, time.Now()); wait > 0 {
			result.retryAfter = wait
			return result, errors.Join(notReady, lastErr), nil
		}
		if err := r.removeFrom(ctx, target, gateway, gatewayID); err != nil {
			if _, podsFailed := errors.AsType[gwadminsync.PodsError](err); !podsFailed {
				return result, notReady, err
			}
			removeErr := fmt.Errorf("failed to make gw-admin stop serving the gateway: %w", err)
			result.retryAfter, result.failed = r.backoff.Failed(gateway.UID, inputs, removeErr, time.Now()), true
			return result, errors.Join(notReady, removeErr), nil
		}
	}
	r.backoff.Forget(gateway.UID)
	r.removedFromGWAdmin.Store(gateway.UID, struct{}{})
	return result, notReady, nil
}

// cleanup makes every gw-admin pod stop serving the gateway, then removes the
// finalizer. When no gw-admin pod can be told, e.g. the GWAdmin doesn't exist
// (anymore), has no pod, or never published its API (see
// gwadminsync.NotReadyError.NothingToRemove), there is nothing to clean up.
// When its pods can't be told yet, e.g. one fails or its TLS material is
// missing, the deletion waits for them up to cleanupGracePeriod from the first
// attempt, which the gateway records, so that a restart of the operator
// doesn't start it over, trying again every cleanupRetryInterval at most.
func (r *Reconciler) cleanup(ctx context.Context, logger logr.Logger, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(gateway, FinalizerGWAdminCleanup) {
		return ctrl.Result{}, nil
	}
	// The deletion of the gateway goes on when the status of its entities
	// can't be set: it would be held by them otherwise.
	if err := r.reportEntitiesNotProgrammed(ctx, gateway, fmt.Sprintf("The OnPremEventGateway %s is being deleted", client.ObjectKeyFromObject(gateway))); err != nil {
		log.Error(logger, err, "failed to report the deletion of the OnPremEventGateway on its entities")
	}
	started, err := r.cleanupStarted(ctx, gateway)
	if err != nil {
		return ctrl.Result{}, err
	}
	target, removeErr := r.removalTarget(ctx, gateway)
	if removeErr == nil && target != nil {
		removeErr = r.removeFrom(ctx, target, gateway, gatewayID)
	}
	if removeErr != nil {
		if remaining := cleanupGracePeriod - time.Since(started); remaining > 0 {
			log.Debug(logger, "gw-admin can't be told to stop serving the deleted gateway yet, retrying",
				"remainingGracePeriod", remaining.String(), "reason", removeErr.Error())
			return ctrl.Result{RequeueAfter: max(min(remaining, cleanupRetryInterval), minCleanupRetryInterval)}, nil
		}
	}
	_, res, err := patch.WithoutFinalizer(ctx, r.Client, gateway, FinalizerGWAdminCleanup)
	if apierrors.IsNotFound(err) {
		// Deleted already, e.g. by a reconcile of a stale gateway.
		return ctrl.Result{}, nil
	}
	if err != nil || !res.IsZero() {
		return res, err
	}
	if removeErr != nil {
		// Reported once the finalizer is removed: a finalizer patch that
		// conflicts is tried again without reporting it twice.
		log.Info(logger, "gw-admin can't be told to stop serving the deleted gateway, deleted it anyway",
			"gracePeriod", cleanupGracePeriod.String(), "reason", removeErr.Error())
		r.event(gateway, corev1.EventTypeWarning, eventReasonCleanupTimedOut, eventActionDelete,
			fmt.Sprintf("Deleted although some gw-admin pod may still serve it: none could be told to stop within %s: %v", cleanupGracePeriod, removeErr))
	}
	return res, nil
}

// cleanupStarted returns when the cleanup of the deleted gateway started,
// which it records on its first attempt.
func (r *Reconciler) cleanupStarted(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway) (time.Time, error) {
	if started, err := time.Parse(time.RFC3339, gateway.GetAnnotations()[annotationCleanupStarted]); err == nil {
		return started, nil
	}
	now := time.Now()
	old := gateway.DeepCopy()
	if gateway.Annotations == nil {
		gateway.Annotations = map[string]string{}
	}
	gateway.Annotations[annotationCleanupStarted] = now.UTC().Format(time.RFC3339)
	if err := r.Patch(ctx, gateway, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		return time.Time{}, fmt.Errorf("failed to record the start of the cleanup: %w", err)
	}
	return now, nil
}

// removalTarget returns the gw-admin to tell to stop serving the gateway, e.g.
// because the gateway is deleted or may not use the GWAdmin anymore, or nil
// when there is nothing to remove (see gwAdminNotReadyError.nothingToRemove).
// It returns a gwAdminNotReadyError when its pods can't be told yet.
func (r *Reconciler) removalTarget(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway) (*gwadminsync.Target, error) {
	target, err := r.resolveGWAdmin(ctx, gateway, false)
	if notReady, ok := errors.AsType[gwAdminNotReadyError](err); ok && notReady.nothingToRemove {
		return nil, nil
	}
	return target, err
}

// removeFrom makes every gw-admin pod of the target stop serving the gateway.
// A pod still pending serves nothing: gw-admin keeps nothing across a
// restart. It returns a gwadminsync.PodsError when a gw-admin pod failed.
func (r *Reconciler) removeFrom(ctx context.Context, target *gwadminsync.Target, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID) error {
	expected, err := expectedGateways(ctx, r.Client, gateway)
	if err != nil {
		return err
	}
	_, err = r.GWAdminSync.ForEachPod(ctx, target, slices.Concat(target.Pods, target.Terminating),
		func(ctx context.Context, pod gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface) error {
			return r.removeGateway(ctx, target, pod, gwAdmin, gatewayID, expected)
		})
	return err
}

// forgetDeleted lets through every change to a gateway, and forgets what the
// reconciles remember of a deleted one.
func (r *Reconciler) forgetDeleted() predicate.Funcs {
	return predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			r.forget(e.Object.GetUID())
			return true
		},
	}
}

// forget drops what the reconciles remember of a deleted gateway.
func (r *Reconciler) forget(uid types.UID) {
	r.removedFromGWAdmin.Delete(uid)
	r.lastEntities.Delete(uid)
	r.reported.Delete(uid)
	r.backoff.Forget(uid)
	r.pendingRetries().Forget(uid)
}

// onlyDeletions lets through the deletions only.
func onlyDeletions() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		UpdateFunc:  func(event.UpdateEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
