package controllers

import (
	"context"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	dataplanevalidation "github.com/kong/gateway-operator/internal/validation/dataplane"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler
// -----------------------------------------------------------------------------

// DataPlaneReconciler reconciles a DataPlane object
type DataPlaneReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {

	r.eventRecorder = mgr.GetEventRecorderFor("dataplane")
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.DataPlane{}).
		Named("DataPlane").
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *DataPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("DataPlane")

	debug(log, "reconciling DataPlane resource", req)
	dataplane := new(operatorv1alpha1.DataPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "validating DataPlane resource conditions", dataplane)
	changed, err := r.ensureDataPlaneIsMarkedScheduled(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		debug(log, "DataPlane resource now marked as scheduled", dataplane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	debug(log, "validating DataPlane configuration", dataplane)
	if len(dataplane.Spec.Env) == 0 && len(dataplane.Spec.EnvFrom) == 0 {
		debug(log, "no ENV config found for DataPlane resource, setting defaults", dataplane)
		dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneDeploymentOptions)
		if err := r.Client.Update(ctx, dataplane); err != nil {
			if k8serrors.IsConflict(err) {
				debug(log, "conflict found when updating DataPlane resource, retrying", dataplane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	// validate dataplane
	err = dataplanevalidation.NewValidator(r.Client).Validate(dataplane)
	if err != nil {
		info(log, "failed to validate dataplane: "+err.Error(), dataplane)
		r.eventRecorder.Event(dataplane, "Warning", "ValidationFailed", err.Error())
		markErr := r.ensureDataPlaneIsMarkedNotProvisioned(ctx, dataplane,
			DataPlaneConditionValidationFailed, err.Error())
		return ctrl.Result{}, markErr
	}

	debug(log, "looking for existing deployments for DataPlane resource", dataplane)
	created, dataplaneDeployment, err := r.ensureDeploymentForDataPlane(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update owned deployment https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of DataPlane deployments", dataplane)
	if dataplaneDeployment.Status.Replicas == 0 || dataplaneDeployment.Status.AvailableReplicas < dataplaneDeployment.Status.Replicas {
		debug(log, "deployment for DataPlane not yet ready, waiting", dataplane)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "exposing DataPlane deployment via service", dataplane)
	created, dataplaneService, err := r.ensureServiceForDataPlane(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update owned service https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of DataPlane service", dataplaneService)
	if dataplaneService.Spec.ClusterIP == "" {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "reconciliation complete for DataPlane resource", dataplane)
	if err := r.ensureDataPlaneIsMarkedProvisioned(ctx, dataplane); err != nil {
		if k8serrors.IsConflict(err) {
			// no need to throw an error for 409's, just requeue to get a fresh copy
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
