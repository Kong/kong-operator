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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ControllerName is the name used for logging.
const ControllerName = "onprem-eventgateway"

// Reconciler reconciles an OnPremEventGateway object. One reconcile serves
// one gateway: a change to the gateway, or to any Event Gateway entity that
// belongs to it, directly or through its parent entities, triggers it.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	// Every change to the gateway triggers it, its status too, so a status
	// changed by someone else is set back. The status write of the reconcile
	// triggers it once more: the status is then unchanged and not written.
	b := ctrl.NewControllerManagedBy(mgr).
		For(&eventgatewayv1alpha1.OnPremEventGateway{})

	// An update is mapped for both the old and the new object, so an entity
	// whose parent reference changes, which the CRDs only allow while it is not
	// Programmed, triggers both gateways. A delete is mapped for the deleted
	// object. GenerationChangedPredicate ignores the status writes on the
	// entities, e.g. the Programmed condition the gateway reconcile will set.
	mapToGateway := handler.EnqueueRequestsFromMapFunc(mapEntityToOnPremEventGateway(mgr.GetClient()))
	for _, entity := range entityTypes() {
		b = b.Watches(entity, mapToGateway, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	}

	return b.Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of an OnPremEventGateway toward the
// desired state.
//
// TODO: build the configuration of the gateway and push it to gw-admin. Until
// then the gateway reports that its configuration has not been pushed.
func (r *Reconciler) Reconcile(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling OnPremEventGateway resource")

	// Nothing is pushed to gw-admin yet, so there is nothing to clean up and
	// the gateway has no finalizer.
	if !gateway.DeletionTimestamp.IsZero() {
		log.Debug(logger, "OnPremEventGateway is being deleted")
		return ctrl.Result{}, nil
	}

	// This controller owns the whole status: it replaces it with the status
	// it computes, dropping anything someone else wrote. The current
	// Programmed condition is carried over, so its lastTransitionTime is kept
	// when nothing changed.
	old := gateway.DeepCopy()
	status := eventgatewayv1alpha1.OnPremEventGatewayStatus{
		ObservedGeneration: gateway.GetGeneration(),
	}
	if current := apimeta.FindStatusCondition(gateway.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)); current != nil {
		status.Conditions = []metav1.Condition{*current}
	}
	gateway.Status = status
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
			metav1.ConditionFalse,
			eventgatewayv1alpha1.OnPremEventGatewayPendingReason,
			eventgatewayv1alpha1.OnPremEventGatewayPendingMessage,
			gateway.GetGeneration(),
		),
		gateway,
	)

	// A JSON merge patch replaces the conditions list as a whole.
	if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, gateway, old); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for OnPremEventGateway resource")

	return ctrl.Result{}, nil
}
