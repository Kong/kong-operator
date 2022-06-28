package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
)

// -----------------------------------------------------------------------------
// ControlPlaneReconciler
// -----------------------------------------------------------------------------

// ControlPlaneReconciler reconciles a ControlPlane object
type ControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *ControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.ControlPlane{}).
		Named("ControlPlane").
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *ControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithName("ControlPlane")

	debug(log, "reconciling ControlPlane resource", req)
	controlplane := new(operatorv1alpha1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, controlplane); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "validating ControlPlane resource conditions", controlplane)
	changed, err := r.ensureControlPlaneIsMarkedScheduled(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		debug(log, "ControlPlane resource now marked as scheduled", controlplane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	debug(log, "validating ControlPlane configuration", controlplane)
	if len(controlplane.Spec.Env) == 0 && len(controlplane.Spec.EnvFrom) == 0 {
		debug(log, "no ENV config found for ControlPlane resource, setting defaults", controlplane)
		setControlPlaneDefaults(&controlplane.Spec.ControlPlaneDeploymentOptions, controlplane.Name, nil)
		if err := r.Client.Update(ctx, controlplane); err != nil {
			if errors.IsConflict(err) {
				debug(log, "conflict found when updating ControlPlane resource, retrying", controlplane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	debug(log, "looking for existing deployments for ControlPlane resource", controlplane)
	created, controlplaneDeployment, err := r.ensureDeploymentForControlPlane(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update sub-resources https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of ControlPlane deployments", controlplane)
	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		debug(log, "deployment for ControlPlane not yet ready, waiting", controlplane)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "reconciliation complete for ControlPlane resource", controlplane)
	return ctrl.Result{}, r.ensureControlPlaneIsMarkedProvisioned(ctx, controlplane)
}
