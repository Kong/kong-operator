package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
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
	Scheme                   *runtime.Scheme
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	DevelopmentMode          bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *ControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// for owned objects we need to check if updates to the objects resulted in the
	// removal of an OwnerReference to the parent object, and if so we need to
	// enqueue the parent object so that reconciliation can create a replacement.
	clusterRolePredicate := predicate.NewPredicateFuncs(r.clusterRoleHasControlplaneOwner)
	clusterRolePredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleHasControlplaneOwner(e.ObjectOld)
	}
	clusterRoleBindingPredicate := predicate.NewPredicateFuncs(r.clusterRoleBindingHasControlplaneOwner)
	clusterRoleBindingPredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleBindingHasControlplaneOwner(e.ObjectOld)
	}

	return ctrl.NewControllerManagedBy(mgr).
		// watch Controlplane objects
		For(&operatorv1alpha1.ControlPlane{}).
		// watch for changes in Secrets created by the controlplane controller
		Owns(&corev1.Secret{}).
		// watch for changes in ServiceAccounts created by the controlplane controller
		Owns(&corev1.ServiceAccount{}).
		// watch for changes in Deployments created by the controlplane controller
		Owns(&appsv1.Deployment{}).
		// watch for changes in ClusterRoles created by the controlplane controller.
		// Since the ClusterRoles are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&rbacv1.ClusterRole{},
			handler.EnqueueRequestsFromMapFunc(r.getControlplaneForClusterRole),
			builder.WithPredicates(clusterRolePredicate)).
		// watch for changes in ClusterRoleBindings created by the controlplane controller.
		// Since the ClusterRoleBindings are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&rbacv1.ClusterRoleBinding{},
			handler.EnqueueRequestsFromMapFunc(r.getControlplaneForClusterRoleBinding),
			builder.WithPredicates(clusterRoleBindingPredicate)).
		Watches(
			&operatorv1alpha1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlanesFromDataPlane)).
		// watch for changes in the DataPlane deployments, as we want to be aware of all
		// the DataPlane pod changes (every time a new pod gets ready, the deployment
		// status gets updated accordingly, leading to a reconciliation loop trigger)
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlanesFromDataPlaneDeployment)).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *ControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := getLogger(ctx, "controlplane", r.DevelopmentMode)

	trace(log, "reconciling ControlPlane resource", req)
	controlplane := new(operatorv1alpha1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, controlplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// controlplane is deleted, just run garbage collection for cluster wide resources.
	if !controlplane.DeletionTimestamp.IsZero() {
		// wait for termination grace period before cleaning up roles and bindings
		if controlplane.DeletionTimestamp.After(metav1.Now().Time) {
			debug(log, "control plane deletion still under grace period", controlplane)
			return ctrl.Result{
				Requeue: true,
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(controlplane.DeletionTimestamp.Time),
			}, nil
		}

		trace(log, "controlplane marked for deletion, removing owned cluster roles and cluster role bindings", controlplane)

		newControlplane := controlplane.DeepCopy()
		// ensure that the clusterrolebindings which were created for the ControlPlane are deleted
		deletions, err := r.ensureOwnedClusterRoleBindingsDeleted(ctx, controlplane)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			debug(log, "clusterRoleBinding deleted", controlplane)
			return ctrl.Result{}, nil // ClusterRoleBinding deletion will requeue
		}

		// now that ClusterRoleBindings are cleaned up, remove the relevant finalizer
		if k8sutils.RemoveFinalizerInMetadata(&newControlplane.ObjectMeta, string(ControlPlaneFinalizerCleanupClusterRoleBinding)) {
			if err := r.Client.Patch(ctx, newControlplane, client.MergeFrom(controlplane)); err != nil {
				return ctrl.Result{}, err
			}
			debug(log, "clusterRoleBinding finalizer removed", controlplane)
			return ctrl.Result{}, nil // Controlplane update will requeue
		}

		// ensure that the clusterroles created for the controlplane are deleted
		deletions, err = r.ensureOwnedClusterRolesDeleted(ctx, controlplane)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			debug(log, "clusterRole deleted", controlplane)
			return ctrl.Result{}, nil // ClusterRole deletion will requeue
		}

		// now that ClusterRoles are cleaned up, remove the relevant finalizer
		if k8sutils.RemoveFinalizerInMetadata(&newControlplane.ObjectMeta, string(ControlPlaneFinalizerCleanupClusterRole)) {
			if err := r.Client.Patch(ctx, newControlplane, client.MergeFrom(controlplane)); err != nil {
				return ctrl.Result{}, err
			}
			debug(log, "clusterRole finalizer removed", controlplane)
			return ctrl.Result{}, nil // Controlplane update will requeue
		}

		// cleanup completed
		debug(log, "resource cleanup completed, controlplane deleted", controlplane)
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	finalizersChanged := k8sutils.EnsureFinalizersInMetadata(&controlplane.ObjectMeta,
		string(ControlPlaneFinalizerCleanupClusterRole),
		string(ControlPlaneFinalizerCleanupClusterRoleBinding))
	if finalizersChanged {
		trace(log, "update metadata of control plane to set finalizer", controlplane)
		if err := r.Client.Update(ctx, controlplane); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizers : %w", err)
		}
		return ctrl.Result{}, nil
	}

	k8sutils.InitReady(controlplane)

	trace(log, "validating ControlPlane resource conditions", controlplane)
	if r.ensureIsMarkedScheduled(controlplane) {
		err := r.patchStatus(ctx, log, controlplane)
		if err != nil {
			debug(log, "unable to update ControlPlane resource", controlplane)
			return ctrl.Result{}, err
		}

		debug(log, "ControlPlane resource now marked as scheduled", controlplane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	trace(log, "retrieving connected dataplane", controlplane)
	dataplane, err := gatewayutils.GetDataPlaneForControlPlane(ctx, r.Client, controlplane)
	var dataplaneProxyServiceName, dataplaneAdminServiceName string
	var dataPlanePodIP string
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}
		debug(log, "no existing dataplane for controlplane", controlplane, "error", err)
	} else {
		dataplaneProxyServiceName, err = gatewayutils.GetDataplaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneProxyServiceLabelValue)
		if err != nil {
			debug(log, "no existing dataplane proxy service for controlplane", controlplane, "error", err)
			return ctrl.Result{}, err
		}

		dataplaneAdminServiceName, err = gatewayutils.GetDataplaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneAdminServiceLabelValue)
		if err != nil {
			debug(log, "no existing dataplane admin service for controlplane", controlplane, "error", err)
			return ctrl.Result{}, err
		}

		trace(log, "retrieving the newest DataPlane pod", controlplane)
		dataPlanePod, err := getDataPlanePod(ctx, r.Client, dataplane.Name, dataplane.Namespace)
		if err != nil {
			return ctrl.Result{}, err
		}
		trace(log, "retrieving the newest DataPlane pod", controlplane, "dataplane", dataPlanePod.Name)
		dataPlanePodIP = dataPlanePod.Status.PodIP
	}

	trace(log, "validating ControlPlane configuration", controlplane)
	// TODO: complete validation here: https://github.com/Kong/gateway-operator/issues/109
	if err := validateControlPlane(controlplane, r.DevelopmentMode); err != nil {
		return ctrl.Result{}, err
	}

	trace(log, "configuring ControlPlane resource", controlplane)
	changed := setControlPlaneDefaults(
		&controlplane.Spec.ControlPlaneOptions,
		nil,
		controlPlaneDefaultsArgs{
			dataPlanePodIP:            dataPlanePodIP,
			namespace:                 controlplane.Namespace,
			dataplaneProxyServiceName: dataplaneProxyServiceName,
			dataplaneAdminServiceName: dataplaneAdminServiceName,
		})
	if changed {
		debug(log, "updating ControlPlane resource after defaults are set since resource has changed", controlplane)
		err := r.Client.Update(ctx, controlplane)
		if err != nil {
			if k8serrors.IsConflict(err) {
				debug(log, "conflict found when updating ControlPlane resource, retrying", controlplane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	trace(log, "validating that the ControlPlane's DataPlane configuration is up to date", controlplane)
	if err = r.ensureDataPlaneConfiguration(ctx, controlplane, dataplaneProxyServiceName); err != nil {
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

	trace(log, "validating ControlPlane's DataPlane status", controlplane)
	dataplaneIsSet := r.ensureDataPlaneStatus(controlplane, dataplane)
	if dataplaneIsSet {
		trace(log, "DataPlane is set, deployment for ControlPlane will be provisioned", controlplane)
	} else {
		debug(log, "DataPlane not set, deployment for ControlPlane will remain dormant", controlplane)
	}

	trace(log, "ensuring ServiceAccount for ControlPlane deployment exists", controlplane)
	createdOrUpdated, controlplaneServiceAccount, err := r.ensureServiceAccountForControlPlane(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "serviceAccount updated", controlplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "ensuring ClusterRoles for ControlPlane deployment exist", controlplane)
	createdOrUpdated, controlplaneClusterRole, err := r.ensureClusterRoleForControlPlane(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "clusterRole updated", controlplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "ensuring that ClusterRoleBindings for ControlPlane Deployment exist", controlplane)
	createdOrUpdated, _, err = r.ensureClusterRoleBindingForControlPlane(ctx, controlplane, controlplaneServiceAccount.Name, controlplaneClusterRole.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "clusterRoleBinding updated", controlplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "creating mTLS certificate", controlplane)
	created, certSecret, err := r.ensureCertificate(ctx, controlplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		debug(log, "mTLS certificate created", controlplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "looking for existing Deployments for ControlPlane resource", controlplane)
	createdOrUpdated, controlplaneDeployment, err := r.ensureDeploymentForControlPlane(ctx, controlplane, controlplaneServiceAccount.Name, certSecret.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "deployment updated", controlplane)
		if !dataplaneIsSet {
			debug(log, "DataPlane not set, deployment for ControlPlane has been scaled down to 0 replicas", controlplane)
			err := r.patchStatus(ctx, log, controlplane)
			if err != nil {
				debug(log, "unable to reconcile ControlPlane status", controlplane)
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}
	trace(log, "checking readiness of ControlPlane deployments", controlplane)

	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		trace(log, "deployment for ControlPlane not ready yet", controlplaneDeployment)
		// Set Ready to false for controlplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			controlplane,
		)
		return ctrl.Result{}, r.patchStatus(ctx, log, controlplane)
	}

	r.ensureIsMarkedProvisioned(controlplane)

	if err = r.patchStatus(ctx, log, controlplane); err != nil {
		debug(log, "unable to reconcile the ControlPlane resource", controlplane)
		return ctrl.Result{}, err
	}

	debug(log, "reconciliation complete for ControlPlane resource", controlplane)
	return ctrl.Result{}, nil
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *ControlPlaneReconciler) patchStatus(ctx context.Context, log logr.Logger, updated *operatorv1alpha1.ControlPlane) error {
	current := &operatorv1alpha1.ControlPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		debug(log, "patching ControlPlane status", updated, "status", updated.Status)
		return r.Client.Status().Patch(ctx, updated, client.MergeFrom(current))
	}

	return nil
}
