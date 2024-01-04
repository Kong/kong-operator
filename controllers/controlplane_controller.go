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
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/controlplane"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/versions"
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
			&operatorv1beta1.DataPlane{},
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
	logger := log.GetLogger(ctx, "controlplane", r.DevelopmentMode)

	log.Trace(logger, "reconciling ControlPlane resource", req)
	controlPlane := new(operatorv1alpha1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, controlPlane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// controlplane is deleted, just run garbage collection for cluster wide resources.
	if !controlPlane.DeletionTimestamp.IsZero() {
		// wait for termination grace period before cleaning up roles and bindings
		if controlPlane.DeletionTimestamp.After(metav1.Now().Time) {
			log.Debug(logger, "control plane deletion still under grace period", controlPlane)
			return ctrl.Result{
				Requeue: true,
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(controlPlane.DeletionTimestamp.Time),
			}, nil
		}

		log.Trace(logger, "controlplane marked for deletion, removing owned cluster roles and cluster role bindings", controlPlane)

		newControlplane := controlPlane.DeepCopy()
		// ensure that the clusterrolebindings which were created for the ControlPlane are deleted
		deletions, err := r.ensureOwnedClusterRoleBindingsDeleted(ctx, controlPlane)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRoleBinding deleted", controlPlane)
			return ctrl.Result{}, nil // ClusterRoleBinding deletion will requeue
		}

		// now that ClusterRoleBindings are cleaned up, remove the relevant finalizer
		if k8sutils.RemoveFinalizerInMetadata(&newControlplane.ObjectMeta, string(ControlPlaneFinalizerCleanupClusterRoleBinding)) {
			if err := r.Client.Patch(ctx, newControlplane, client.MergeFrom(controlPlane)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRoleBinding finalizer removed", controlPlane)
			return ctrl.Result{}, nil // Controlplane update will requeue
		}

		// ensure that the clusterroles created for the controlplane are deleted
		deletions, err = r.ensureOwnedClusterRolesDeleted(ctx, controlPlane)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRole deleted", controlPlane)
			return ctrl.Result{}, nil // ClusterRole deletion will requeue
		}

		// now that ClusterRoles are cleaned up, remove the relevant finalizer
		if k8sutils.RemoveFinalizerInMetadata(&newControlplane.ObjectMeta, string(ControlPlaneFinalizerCleanupClusterRole)) {
			if err := r.Client.Patch(ctx, newControlplane, client.MergeFrom(controlPlane)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRole finalizer removed", controlPlane)
			return ctrl.Result{}, nil // Controlplane update will requeue
		}

		// cleanup completed
		log.Debug(logger, "resource cleanup completed, controlplane deleted", controlPlane)
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	finalizersChanged := k8sutils.EnsureFinalizersInMetadata(&controlPlane.ObjectMeta,
		string(ControlPlaneFinalizerCleanupClusterRole),
		string(ControlPlaneFinalizerCleanupClusterRoleBinding))
	if finalizersChanged {
		log.Trace(logger, "update metadata of control plane to set finalizer", controlPlane)
		if err := r.Client.Update(ctx, controlPlane); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizers : %w", err)
		}
		return ctrl.Result{}, nil
	}

	k8sutils.InitReady(controlPlane)

	log.Trace(logger, "validating ControlPlane resource conditions", controlPlane)
	if r.ensureIsMarkedScheduled(controlPlane) {
		err := r.patchStatus(ctx, logger, controlPlane)
		if err != nil {
			log.Debug(logger, "unable to update ControlPlane resource", controlPlane)
			return ctrl.Result{}, err
		}

		log.Debug(logger, "ControlPlane resource now marked as scheduled", controlPlane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	log.Trace(logger, "retrieving connected dataplane", controlPlane)
	dataplane, err := gatewayutils.GetDataPlaneForControlPlane(ctx, r.Client, controlPlane)
	var dataplaneIngressServiceName, dataplaneAdminServiceName string
	var dataPlanePodIP string
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}
		log.Debug(logger, "no existing dataplane for controlplane", controlPlane, "error", err)
	} else {
		dataplaneIngressServiceName, err = gatewayutils.GetDataplaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneIngressServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane ingress service for controlplane", controlPlane, "error", err)
			return ctrl.Result{}, err
		}

		dataplaneAdminServiceName, err = gatewayutils.GetDataplaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneAdminServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane admin service for controlplane", controlPlane, "error", err)
			return ctrl.Result{}, err
		}

		log.Trace(logger, "retrieving the newest DataPlane pod", controlPlane)
		dataPlanePod, err := getDataPlanePod(ctx, r.Client, dataplane.Name, dataplane.Namespace)
		if err != nil {
			return ctrl.Result{}, err
		}
		log.Trace(logger, "retrieving the newest DataPlane pod", controlPlane, "dataplane", dataPlanePod.Name)
		dataPlanePodIP = dataPlanePod.Status.PodIP
	}

	log.Trace(logger, "validating ControlPlane configuration", controlPlane)
	// TODO: complete validation here: https://github.com/Kong/gateway-operator/issues/109
	if err := validateControlPlane(controlPlane, r.DevelopmentMode); err != nil {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "configuring ControlPlane resource", controlPlane)
	changed := controlplane.SetDefaults(
		&controlPlane.Spec.ControlPlaneOptions,
		nil,
		controlplane.DefaultsArgs{
			DataPlanePodIP:              dataPlanePodIP,
			Namespace:                   controlPlane.Namespace,
			DataplaneIngressServiceName: dataplaneIngressServiceName,
			DataplaneAdminServiceName:   dataplaneAdminServiceName,
		})
	if changed {
		log.Debug(logger, "updating ControlPlane resource after defaults are set since resource has changed", controlPlane)
		err := r.Client.Update(ctx, controlPlane)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane resource, retrying", controlPlane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	log.Trace(logger, "validating that the ControlPlane's DataPlane configuration is up to date", controlPlane)
	if err = r.ensureDataPlaneConfiguration(ctx, controlPlane, dataplaneIngressServiceName); err != nil {
		if k8serrors.IsConflict(err) {
			log.Debug(
				logger,
				"conflict found when trying to ensure ControlPlane's DataPlane configuration was up to date, retrying",
				controlPlane,
			)
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	log.Trace(logger, "validating ControlPlane's DataPlane status", controlPlane)
	dataplaneIsSet := r.ensureDataPlaneStatus(controlPlane, dataplane)
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned", controlPlane)
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant", controlPlane)
	}

	log.Trace(logger, "ensuring ServiceAccount for ControlPlane deployment exists", controlPlane)
	createdOrUpdated, controlplaneServiceAccount, err := r.ensureServiceAccountForControlPlane(ctx, controlPlane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "serviceAccount updated", controlPlane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring ClusterRoles for ControlPlane deployment exist", controlPlane)
	createdOrUpdated, controlplaneClusterRole, err := r.ensureClusterRoleForControlPlane(ctx, controlPlane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRole updated", controlPlane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring that ClusterRoleBindings for ControlPlane Deployment exist", controlPlane)
	createdOrUpdated, _, err = r.ensureClusterRoleBindingForControlPlane(ctx, controlPlane, controlplaneServiceAccount.Name, controlplaneClusterRole.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRoleBinding updated", controlPlane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "creating mTLS certificate", controlPlane)
	res, certSecret, err := r.ensureCertificate(ctx, controlPlane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		log.Debug(logger, "mTLS certificate created/updated", controlPlane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "looking for existing Deployments for ControlPlane resource", controlPlane)
	res, controlplaneDeployment, err := r.ensureDeploymentForControlPlane(ctx, logger, controlPlane, controlplaneServiceAccount.Name, certSecret.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		if !dataplaneIsSet {
			log.Debug(logger, "DataPlane not set, deployment for ControlPlane has been scaled down to 0 replicas", controlPlane)
			err := r.patchStatus(ctx, logger, controlPlane)
			if err != nil {
				log.Debug(logger, "unable to reconcile ControlPlane status", controlPlane)
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}
	log.Trace(logger, "checking readiness of ControlPlane deployments", controlPlane)

	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		log.Trace(logger, "deployment for ControlPlane not ready yet", controlplaneDeployment)
		// Set Ready to false for controlplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			controlPlane,
		)
		return ctrl.Result{}, r.patchStatus(ctx, logger, controlPlane)
	}

	markAsProvisioned(controlPlane)
	k8sutils.SetReady(controlPlane)

	if err = r.patchStatus(ctx, logger, controlPlane); err != nil {
		log.Debug(logger, "unable to reconcile the ControlPlane resource", controlPlane)
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for ControlPlane resource", controlPlane)
	return ctrl.Result{}, nil
}

// validateControlPlane validates the control plane.
func validateControlPlane(controlPlane *operatorv1alpha1.ControlPlane, devMode bool) error {
	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !devMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneImageVersionSupported)
	}
	_, err := controlplane.GenerateImage(&controlPlane.Spec.ControlPlaneOptions, versionValidationOptions...)
	return err
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *ControlPlaneReconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *operatorv1alpha1.ControlPlane) error {
	current := &operatorv1alpha1.ControlPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		log.Debug(logger, "patching ControlPlane status", updated, "status", updated.Status)
		return r.Client.Status().Patch(ctx, updated, client.MergeFrom(current))
	}

	return nil
}
