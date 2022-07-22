package controllers

import (
	"context"
	"errors"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
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
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	k8sutils.InitReady(controlplane)

	debug(log, "validating ControlPlane resource conditions", controlplane)
	if r.ensureIsMarkedScheduled(controlplane) {
		err := r.updateStatus(ctx, controlplane)
		if err != nil {
			debug(log, "unable to update ControlPlane resource", controlplane)
			return ctrl.Result{}, err
		}

		debug(log, "ControlPlane resource now marked as scheduled", controlplane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	debug(log, "retrieving dataplane service info", controlplane)
	dataplaneServiceName, err := gatewayutils.GetDataplaneServiceNameForControlplane(ctx, r.Client, controlplane)
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}

		debug(log, "no existing dataplane for controlplane", controlplane, "error", err)
	}

	debug(log, "validating ControlPlane configuration", controlplane)
	if len(controlplane.Spec.Env) == 0 && len(controlplane.Spec.EnvFrom) == 0 {
		debug(log, "no ENV config found for ControlPlane resource, setting defaults", controlplane)
		setControlPlaneDefaults(&controlplane.Spec.ControlPlaneDeploymentOptions, controlplane.Namespace, dataplaneServiceName, nil)
		err := r.Client.Update(ctx, controlplane)
		if err != nil {
			if k8serrors.IsConflict(err) {
				debug(log, "conflict found when updating ControlPlane resource, retrying", controlplane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
		}
		return ctrl.Result{}, err // no need to requeue, the update will trigger.
	}

	debug(log, "validating that the ControlPlane's DataPlane configuration is up to date", controlplane)
	if err = r.ensureDataPlaneConfiguration(ctx, controlplane, dataplaneServiceName); err != nil {
		if k8serrors.IsConflict(err) {
			debug(
				log,
				"conflict found when trying to ensure ControlPlane's DataPlane configuration was up to date, retrying",
				controlplane,
			)
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "validating ControlPlane's DataPlane status", controlplane)
	dataplaneIsSet := r.ensureDataPlaneStatus(controlplane)
	if dataplaneIsSet {
		debug(log, "DataPlane was set, deployment for ControlPlane will be provisioned", controlplane)
	} else {
		debug(log, "DataPlane not set, deployment for ControlPlane will remain dormant", controlplane)
	}

	debug(log, "ensuring ServiceAccount for ControlPlane deployment exists", controlplane)
	created, controlplaneServiceAccount, err := r.ensureServiceAccountForControlPlane(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	debug(log, "ensuring ClusterRoles for ControlPlane deployment exist", controlplane)
	created, controlplaneClusterRole, err := r.ensureClusterRoleForControlPlane(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	debug(log, "ensuring that ClusterRoleBindings for ControlPlane Deployment exist", controlplane)
	created, _, err = r.ensureClusterRoleBindingForControlPlane(ctx, controlplane, controlplaneServiceAccount.Name, controlplaneClusterRole.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	debug(log, "looking for existing Deployments for ControlPlane resource", controlplane)
	mutated, controlplaneDeployment, err := r.ensureDeploymentForControlPlane(ctx, controlplane, controlplaneServiceAccount.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if mutated {
		if !dataplaneIsSet {
			debug(log, "DataPlane not set, deployment for ControlPlane has been scaled down to 0 replicas", controlplane)
			err := r.updateStatus(ctx, controlplane)
			if err != nil {
				debug(log, "unable to reconcile ControlPlane status", controlplane)
			}
			return ctrl.Result{}, err // no need to requeue, status update will requeue
		}
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update sub-resources https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of ControlPlane deployments", controlplane)

	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		debug(log, "deployment for ControlPlane not yet ready, waiting", controlplane)
		return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
	}

	debug(log, "reconciliation complete for ControlPlane resource", controlplane)

	r.ensureIsMarkedProvisioned(controlplane)
	err = r.updateStatus(ctx, controlplane)

	if err != nil {
		if k8serrors.IsConflict(err) {
			// no need to throw an error for 409's, just requeue to get a fresh copy
			debug(log, "conflict during ControlPlane reconciliation", controlplane)
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
		}
		debug(log, "unable to reconcile the ControlPlane resource", controlplane)
	} else {
		debug(log, "reconciliation complete for ControlPlane resource", controlplane)
	}
	return ctrl.Result{}, err
}

// updateStatus Updates the resource status only when there are changes in the Conditions
func (r *ControlPlaneReconciler) updateStatus(ctx context.Context, updated *operatorv1alpha1.ControlPlane) error {
	current := &operatorv1alpha1.ControlPlane{}
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)

	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if k8sutils.NeedsUpdate(current, updated) {
		return r.Client.Status().Update(ctx, updated)
	}
	return nil
}
