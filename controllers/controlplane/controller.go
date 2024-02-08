package controlplane

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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

// Reconciler reconciles a ControlPlane object
type Reconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	DevelopmentMode          bool
}

const requeueWithoutBackoff = time.Millisecond * 200

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	// for owned objects we need to check if updates to the objects resulted in the
	// removal of an OwnerReference to the parent object, and if so we need to
	// enqueue the parent object so that reconciliation can create a replacement.
	clusterRolePredicate := predicate.NewPredicateFuncs(r.clusterRoleHasControlPlaneOwner)
	clusterRolePredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleHasControlPlaneOwner(e.ObjectOld)
	}
	clusterRoleBindingPredicate := predicate.NewPredicateFuncs(r.clusterRoleBindingHasControlPlaneOwner)
	clusterRoleBindingPredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleBindingHasControlPlaneOwner(e.ObjectOld)
	}

	return ctrl.NewControllerManagedBy(mgr).
		// watch ControlPlane objects
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
			handler.EnqueueRequestsFromMapFunc(r.getControlPlaneForClusterRole),
			builder.WithPredicates(clusterRolePredicate)).
		// watch for changes in ClusterRoleBindings created by the controlplane controller.
		// Since the ClusterRoleBindings are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&rbacv1.ClusterRoleBinding{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlaneForClusterRoleBinding),
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
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "controlplane", r.DevelopmentMode)

	log.Trace(logger, "reconciling ControlPlane resource", req)
	cp := new(operatorv1alpha1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, cp); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// controlplane is deleted, just run garbage collection for cluster wide resources.
	if !cp.DeletionTimestamp.IsZero() {
		// wait for termination grace period before cleaning up roles and bindings
		if cp.DeletionTimestamp.After(metav1.Now().Time) {
			log.Debug(logger, "control plane deletion still under grace period", cp)
			return ctrl.Result{
				Requeue: true,
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(cp.DeletionTimestamp.Time),
			}, nil
		}

		log.Trace(logger, "controlplane marked for deletion, removing owned cluster roles and cluster role bindings", cp)

		newControlPlane := cp.DeepCopy()
		// ensure that the clusterrolebindings which were created for the ControlPlane are deleted
		deletions, err := r.ensureOwnedClusterRoleBindingsDeleted(ctx, cp)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRoleBinding deleted", cp)
			return ctrl.Result{}, nil // ClusterRoleBinding deletion will requeue
		}

		// now that ClusterRoleBindings are cleaned up, remove the relevant finalizer
		if controllerutil.RemoveFinalizer(newControlPlane, string(ControlPlaneFinalizerCleanupClusterRoleBinding)) {
			if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRoleBinding finalizer removed", cp)
			return ctrl.Result{}, nil // ControlPlane update will requeue
		}

		// ensure that the clusterroles created for the controlplane are deleted
		deletions, err = r.ensureOwnedClusterRolesDeleted(ctx, cp)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRole deleted", cp)
			return ctrl.Result{}, nil // ClusterRole deletion will requeue
		}

		// now that ClusterRoles are cleaned up, remove the relevant finalizer
		if controllerutil.RemoveFinalizer(newControlPlane, string(ControlPlaneFinalizerCleanupClusterRole)) {
			if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRole finalizer removed", cp)
			return ctrl.Result{}, nil // ControlPlane update will requeue
		}

		// cleanup completed
		log.Debug(logger, "resource cleanup completed, controlplane deleted", cp)
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	crFinalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCleanupClusterRole))
	crbFinalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCleanupClusterRoleBinding))
	if crFinalizerSet || crbFinalizerSet {
		log.Trace(logger, "Setting finalizers", cp)
		if err := r.Client.Update(ctx, cp); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizers : %w", err)
		}
		return ctrl.Result{}, nil
	}

	k8sutils.InitReady(cp)

	log.Trace(logger, "validating ControlPlane resource conditions", cp)
	if r.ensureIsMarkedScheduled(cp) {
		err := r.patchStatus(ctx, logger, cp)
		if err != nil {
			log.Debug(logger, "unable to update ControlPlane resource", cp)
			return ctrl.Result{}, err
		}

		log.Debug(logger, "ControlPlane resource now marked as scheduled", cp)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	log.Trace(logger, "retrieving connected dataplane", cp)
	dataplane, err := gatewayutils.GetDataPlaneForControlPlane(ctx, r.Client, cp)
	var dataplaneIngressServiceName, dataplaneAdminServiceName string
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}
		log.Debug(logger, "no existing dataplane for controlplane", cp, "error", err)
	} else {
		dataplaneIngressServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneIngressServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane ingress service for controlplane", cp, "error", err)
			return ctrl.Result{}, err
		}

		dataplaneAdminServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneAdminServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane admin service for controlplane", cp, "error", err)
			return ctrl.Result{}, err
		}
	}

	log.Trace(logger, "validating ControlPlane configuration", cp)
	// TODO: complete validation here: https://github.com/Kong/gateway-operator/issues/109
	if err := validateControlPlane(cp, r.DevelopmentMode); err != nil {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "configuring ControlPlane resource", cp)
	changed := controlplane.SetDefaults(
		&cp.Spec.ControlPlaneOptions,
		nil,
		controlplane.DefaultsArgs{
			Namespace:                   cp.Namespace,
			ControlPlaneName:            cp.Name,
			DataPlaneIngressServiceName: dataplaneIngressServiceName,
			DataPlaneAdminServiceName:   dataplaneAdminServiceName,
		})
	if changed {
		log.Debug(logger, "updating ControlPlane resource after defaults are set since resource has changed", cp)
		err := r.Client.Update(ctx, cp)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane resource, retrying", cp)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	log.Trace(logger, "validating that the ControlPlane's DataPlane configuration is up to date", cp)
	if err = r.ensureDataPlaneConfiguration(ctx, cp, dataplaneIngressServiceName); err != nil {
		if k8serrors.IsConflict(err) {
			log.Debug(
				logger,
				"conflict found when trying to ensure ControlPlane's DataPlane configuration was up to date, retrying",
				cp,
			)
			return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	log.Trace(logger, "validating ControlPlane's DataPlane status", cp)
	dataplaneIsSet := r.ensureDataPlaneStatus(cp, dataplane)
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned", cp)
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant", cp)
	}

	log.Trace(logger, "ensuring ServiceAccount for ControlPlane deployment exists", cp)
	createdOrUpdated, controlplaneServiceAccount, err := r.ensureServiceAccount(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "serviceAccount updated", cp)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring ClusterRoles for ControlPlane deployment exist", cp)
	createdOrUpdated, controlplaneClusterRole, err := r.ensureClusterRole(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRole updated", cp)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring that ClusterRoleBindings for ControlPlane Deployment exist", cp)
	createdOrUpdated, _, err = r.ensureClusterRoleBinding(ctx, cp, controlplaneServiceAccount.Name, controlplaneClusterRole.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRoleBinding updated", cp)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "creating mTLS certificate", cp)
	res, certSecret, err := r.ensureCertificate(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		log.Debug(logger, "mTLS certificate created/updated", cp)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "looking for existing Deployments for ControlPlane resource", cp)
	res, controlplaneDeployment, err := r.ensureDeployment(ctx, logger, cp, controlplaneServiceAccount.Name, certSecret.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		if !dataplaneIsSet {
			log.Debug(logger, "DataPlane not set, deployment for ControlPlane has been scaled down to 0 replicas", cp)
			err := r.patchStatus(ctx, logger, cp)
			if err != nil {
				log.Debug(logger, "unable to reconcile ControlPlane status", cp)
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}
	log.Trace(logger, "checking readiness of ControlPlane deployments", cp)

	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		log.Trace(logger, "deployment for ControlPlane not ready yet", controlplaneDeployment)
		// Set Ready to false for controlplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			cp,
		)
		return ctrl.Result{}, r.patchStatus(ctx, logger, cp)
	}

	markAsProvisioned(cp)
	k8sutils.SetReady(cp)

	if err = r.patchStatus(ctx, logger, cp); err != nil {
		log.Debug(logger, "unable to reconcile the ControlPlane resource", cp)
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for ControlPlane resource", cp)
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
func (r *Reconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *operatorv1alpha1.ControlPlane) error {
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
