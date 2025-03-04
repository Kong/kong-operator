package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	admregv1 "k8s.io/api/admissionregistration/v1"
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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller"
	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	"github.com/kong/gateway-operator/controller/pkg/extensions"
	extensionserrors "github.com/kong/gateway-operator/controller/pkg/extensions/errors"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/consts"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// Reconciler reconciles a ControlPlane object
type Reconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	ClusterCAKeyConfig       secrets.KeyConfig
	DevelopmentMode          bool
	KonnectEnabled           bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// for owned objects we need to check if updates to the objects resulted in the
	// removal of an OwnerReference to the parent object, and if so we need to
	// enqueue the parent object so that reconciliation can create a replacement.
	clusterRoleOwnerPredicate := predicate.NewPredicateFuncs(r.clusterRoleHasControlPlaneOwner)
	clusterRoleOwnerPredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleHasControlPlaneOwner(e.ObjectOld)
	}
	clusterRoleBindingOwnerPredicate := predicate.NewPredicateFuncs(r.clusterRoleBindingHasControlPlaneOwner)
	clusterRoleBindingOwnerPredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.clusterRoleBindingHasControlPlaneOwner(e.ObjectOld)
	}
	validatinWebhookConfigurationOwnerPredicate := predicate.NewPredicateFuncs(r.validatingWebhookConfigurationHasControlPlaneOwner)
	validatinWebhookConfigurationOwnerPredicate.UpdateFunc = func(e event.UpdateEvent) bool {
		return r.validatingWebhookConfigurationHasControlPlaneOwner(e.ObjectOld)
	}

	return ctrl.NewControllerManagedBy(mgr).
		// watch ControlPlane objects
		For(&operatorv1beta1.ControlPlane{}).
		// watch for changes in Secrets created by the controlplane controller
		Owns(&corev1.Secret{}).
		// watch for changes in ServiceAccounts created by the controlplane controller
		Owns(&corev1.ServiceAccount{}).
		// watch for changes in Deployments created by the controlplane controller
		Owns(&appsv1.Deployment{}).
		// watch for changes in Services created by the controlplane controller
		Owns(&corev1.Service{}).
		// watch for changes in ValidatingWebhookConfigurations created by the controlplane controller.
		// Since the ValidatingWebhookConfigurations are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&admregv1.ValidatingWebhookConfiguration{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlaneForValidatingWebhookConfiguration),
			builder.WithPredicates(validatinWebhookConfigurationOwnerPredicate),
		).
		// watch for changes in ClusterRoles created by the controlplane controller.
		// Since the ClusterRoles are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&rbacv1.ClusterRole{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlaneForClusterRole),
			builder.WithPredicates(clusterRoleOwnerPredicate)).
		// watch for changes in ClusterRoleBindings created by the controlplane controller.
		// Since the ClusterRoleBindings are cluster-wide but controlplanes are namespaced,
		// we need to manually detect the owner by means of the UID
		// (Owns cannot be used in this case)
		Watches(
			&rbacv1.ClusterRoleBinding{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlaneForClusterRoleBinding),
			builder.WithPredicates(clusterRoleBindingOwnerPredicate)).
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

	log.Trace(logger, "reconciling ControlPlane resource")
	cp := new(operatorv1beta1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// controlplane is deleted, just run garbage collection for cluster wide resources.
	if !cp.DeletionTimestamp.IsZero() {
		// wait for termination grace period before cleaning up roles and bindings
		if cp.DeletionTimestamp.After(metav1.Now().Time) {
			log.Debug(logger, "control plane deletion still under grace period")
			return ctrl.Result{
				Requeue: true,
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(cp.DeletionTimestamp.Time),
			}, nil
		}

		log.Trace(logger, "controlplane marked for deletion, removing owned cluster roles, cluster role bindings and validating webhook configurations")

		newControlPlane := cp.DeepCopy()

		// ensure that the ValidatingWebhookConfigurations which was created for the ControlPlane is deleted
		deletions, err := r.ensureOwnedValidatingWebhookConfigurationDeleted(ctx, cp)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "ValidatingWebhookConfiguration deleted")
			return ctrl.Result{}, nil // ValidatingWebhookConfiguration deletion will requeue
		}

		// now that ValidatingWebhookConfigurations are cleaned up, remove the relevant finalizer
		if controllerutil.RemoveFinalizer(newControlPlane, string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration)) {
			if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "ValidatingWebhookConfigurations finalizer removed")
			return ctrl.Result{}, nil // ControlPlane update will requeue
		}

		// ensure that the clusterrolebindings which were created for the ControlPlane are deleted
		deletions, err = r.ensureOwnedClusterRoleBindingsDeleted(ctx, cp)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRoleBinding deleted")
			return ctrl.Result{}, nil // ClusterRoleBinding deletion will requeue
		}

		// now that ClusterRoleBindings are cleaned up, remove the relevant finalizer
		if controllerutil.RemoveFinalizer(newControlPlane, string(ControlPlaneFinalizerCleanupClusterRoleBinding)) {
			if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRoleBinding finalizer removed")
			return ctrl.Result{}, nil // ControlPlane update will requeue
		}

		// ensure that the clusterroles created for the controlplane are deleted
		deletions, err = r.ensureOwnedClusterRolesDeleted(ctx, cp)
		if err != nil {
			return ctrl.Result{}, err
		}
		if deletions {
			log.Debug(logger, "clusterRole deleted")
			return ctrl.Result{}, nil // ClusterRole deletion will requeue
		}

		// now that ClusterRoles are cleaned up, remove the relevant finalizer
		if controllerutil.RemoveFinalizer(newControlPlane, string(ControlPlaneFinalizerCleanupClusterRole)) {
			if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "clusterRole finalizer removed")
			return ctrl.Result{}, nil // ControlPlane update will requeue
		}

		// cleanup completed
		log.Debug(logger, "resource cleanup completed, controlplane deleted")
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	crFinalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCleanupClusterRole))
	crbFinalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCleanupClusterRoleBinding))
	vwcFinalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration))
	if crFinalizerSet || crbFinalizerSet || vwcFinalizerSet {
		log.Trace(logger, "setting finalizers")
		if err := r.Client.Update(ctx, cp); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizers : %w", err)
		}
		// Requeue to ensure that we do not miss next reconciliation request in case
		// AddFinalizer calls returned true but the update resulted in a noop.
		return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
	}

	k8sutils.InitReady(cp)

	log.Trace(logger, "validating ControlPlane resource conditions")
	if r.ensureIsMarkedScheduled(cp) {
		res, err := r.patchStatus(ctx, logger, cp)
		if err != nil {
			log.Debug(logger, "unable to update ControlPlane resource", "error", err)
			return res, err
		}
		if !res.IsZero() {
			log.Debug(logger, "unable to update ControlPlane resource")
			return res, nil
		}

		log.Debug(logger, "ControlPlane resource now marked as scheduled")
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	log.Trace(logger, "retrieving connected dataplane")
	dataplane, err := gatewayutils.GetDataPlaneForControlPlane(ctx, r.Client, cp)
	var dataplaneIngressServiceName, dataplaneAdminServiceName string
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}
		log.Debug(logger, "no existing dataplane for controlplane", "error", err)
	} else {
		dataplaneIngressServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneIngressServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane ingress service for controlplane", "error", err)
			return ctrl.Result{}, err
		}

		dataplaneAdminServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneAdminServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane admin service for controlplane", "error", err)
			return ctrl.Result{}, err
		}
	}

	log.Trace(logger, "validating ControlPlane configuration")
	if err := validateControlPlane(cp, r.DevelopmentMode); err != nil {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "configuring ControlPlane resource")

	defaultArgs := controlplane.DefaultsArgs{
		Namespace:                   cp.Namespace,
		ControlPlaneName:            cp.Name,
		DataPlaneIngressServiceName: dataplaneIngressServiceName,
		DataPlaneAdminServiceName:   dataplaneAdminServiceName,
		AnonymousReportsEnabled:     controlplane.DeduceAnonymousReportsEnabled(r.DevelopmentMode, &cp.Spec.ControlPlaneOptions),
	}
	for _, owner := range cp.OwnerReferences {
		if strings.HasPrefix(owner.APIVersion, gatewayv1.GroupName) && owner.Kind == "Gateway" {
			defaultArgs.OwnedByGateway = owner.Name
			continue
		}
	}
	_ = controlplane.SetDefaults(
		&cp.Spec.ControlPlaneOptions,
		defaultArgs)
	stop, result, err := extensions.ApplyExtensions(ctx, r.Client, logger, cp, r.KonnectEnabled)
	if err != nil {
		if extensionserrors.IsKonnectExtensionError(err) {
			log.Debug(logger, "failed to apply extensions", "err", err)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if stop || !result.IsZero() {
		return ctrl.Result{}, nil
	}

	log.Trace(logger, "validating that the ControlPlane's DataPlane configuration is up to date")
	if err = r.ensureDataPlaneConfiguration(ctx, cp, dataplaneIngressServiceName); err != nil {
		if k8serrors.IsConflict(err) {
			log.Debug(
				logger,
				"conflict found when trying to ensure ControlPlane's DataPlane configuration was up to date, retrying",
				"controlPlane", cp,
			)
			return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	log.Trace(logger, "validating ControlPlane's DataPlane status")
	dataplaneIsSet := r.ensureDataPlaneStatus(cp, dataplane)
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned")
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant")
	}

	log.Trace(logger, "ensuring ServiceAccount for ControlPlane deployment exists")
	createdOrUpdated, controlplaneServiceAccount, err := r.ensureServiceAccount(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "serviceAccount updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring ClusterRoles for ControlPlane deployment exist")
	createdOrUpdated, controlplaneClusterRole, err := r.ensureClusterRole(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRole updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring that ClusterRoleBindings for ControlPlane Deployment exist")
	createdOrUpdated, _, err = r.ensureClusterRoleBinding(ctx, cp, controlplaneServiceAccount.Name, controlplaneClusterRole.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		log.Debug(logger, "clusterRoleBinding updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "creating mTLS certificate")
	res, adminCertificate, err := r.ensureAdminMTLSCertificateSecret(ctx, cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		log.Debug(logger, "mTLS certificate created/updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	deploymentParams := ensureDeploymentParams{
		ControlPlane:            cp,
		ServiceAccountName:      controlplaneServiceAccount.Name,
		AdminMTLSCertSecretName: adminCertificate.Name,
	}

	admissionWebhookCertificateSecretName, res, err := r.ensureWebhookResources(ctx, logger, cp)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure webhook resources: %w", err)
	} else if res != op.Noop {
		return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
	}
	deploymentParams.AdmissionWebhookCertSecretName = admissionWebhookCertificateSecretName

	log.Trace(logger, "looking for existing Deployments for ControlPlane resource")
	res, controlplaneDeployment, err := r.ensureDeployment(ctx, logger, deploymentParams)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		if !dataplaneIsSet {
			log.Debug(logger, "DataPlane not set, deployment for ControlPlane has been scaled down to 0 replicas")
			res, err := r.patchStatus(ctx, logger, cp)
			if err != nil {
				log.Debug(logger, "unable to reconcile ControlPlane status", "error", err)
				return ctrl.Result{}, err
			}
			if !res.IsZero() {
				log.Debug(logger, "unable to update ControlPlane resource")
				return res, nil
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}
	log.Trace(logger, "checking readiness of ControlPlane deployments")

	if controlplaneDeployment.Status.Replicas == 0 || controlplaneDeployment.Status.AvailableReplicas < controlplaneDeployment.Status.Replicas {
		log.Trace(logger, "deployment for ControlPlane not ready yet", "deployment", controlplaneDeployment)
		// Set Ready to false for controlplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewCondition(consts.ReadyType, metav1.ConditionFalse, consts.WaitingToBecomeReadyReason, consts.WaitingToBecomeReadyMessage),
			cp,
		)

		res, err := r.patchStatus(ctx, logger, cp)
		if err != nil {
			log.Debug(logger, "unable to patch ControlPlane status", "error", err)
			return ctrl.Result{}, err
		}
		if !res.IsZero() {
			log.Debug(logger, "unable to patch ControlPlane status")
			return res, nil
		}
		return ctrl.Result{}, nil
	}

	markAsProvisioned(cp)
	k8sutils.SetReady(cp)

	result, err = r.patchStatus(ctx, logger, cp)
	if err != nil {
		log.Debug(logger, "unable to patch ControlPlane status", "error", err)
		return ctrl.Result{}, err
	}
	if !result.IsZero() {
		log.Debug(logger, "unable to patch ControlPlane status")
		return result, nil
	}

	log.Debug(logger, "reconciliation complete for ControlPlane resource")
	return ctrl.Result{}, nil
}

// validateControlPlane validates the control plane.
func validateControlPlane(controlPlane *operatorv1beta1.ControlPlane, devMode bool) error {
	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !devMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneImageVersionSupported)
	}
	_, err := controlplane.GenerateImage(&controlPlane.Spec.ControlPlaneOptions, versionValidationOptions...)
	return err
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *Reconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *operatorv1beta1.ControlPlane) (ctrl.Result, error) {
	current := &operatorv1beta1.ControlPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		log.Debug(logger, "patching ControlPlane status", "status", updated.Status)
		if err := r.Client.Status().Patch(ctx, updated, client.MergeFrom(current)); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's status : %w", err)
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureWebhookResources(
	ctx context.Context, logger logr.Logger, cp *operatorv1beta1.ControlPlane,
) (string, op.Result, error) {
	webhookEnabled := isAdmissionWebhookEnabled(ctx, r.Client, logger, cp)
	if !webhookEnabled {
		log.Debug(logger, "admission webhook disabled, ensuring admission webhook resources are not present")
	} else {
		log.Debug(logger, "admission webhook enabled, enforcing admission webhook resources")
	}

	log.Trace(logger, "ensuring admission webhook service")
	res, admissionWebhookService, err := r.ensureAdmissionWebhookService(ctx, logger, r.Client, cp)
	if err != nil {
		return "", res, fmt.Errorf("failed to ensure admission webhook service: %w", err)
	}
	if res != op.Noop {
		if !webhookEnabled {
			log.Debug(logger, "admission webhook service has been removed")
		} else {
			log.Debug(logger, "admission webhook service has been created/updated")
		}
		return "", res, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring admission webhook certificate")
	res, admissionWebhookCertificateSecret, err := r.ensureAdmissionWebhookCertificateSecret(ctx, logger, cp, admissionWebhookService)
	if err != nil {
		return "", res, err
	}
	if res != op.Noop {
		if !webhookEnabled {
			log.Debug(logger, "admission webhook service certificate has been removed")
		} else {
			log.Debug(logger, "admission webhook service certificate has been created/updated")
		}
		return "", res, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring admission webhook configuration")
	res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, admissionWebhookCertificateSecret, admissionWebhookService)
	if err != nil {
		return "", res, err
	}
	if res != op.Noop {
		if !webhookEnabled {
			log.Debug(logger, "ValidatingWebhookConfiguration has been removed")
		} else {
			log.Debug(logger, "ValidatingWebhookConfiguration has been created/updated")
		}
	}
	if webhookEnabled {
		return admissionWebhookCertificateSecret.Name, res, nil
	}
	return "", res, nil
}

func isAdmissionWebhookEnabled(ctx context.Context, cl client.Client, logger logr.Logger, cp *operatorv1beta1.ControlPlane) bool {
	if cp.Spec.Deployment.PodTemplateSpec == nil {
		return false
	}

	container := k8sutils.GetPodContainerByName(&cp.Spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		return false
	}
	admissionWebhookListen, ok, err := k8sutils.GetEnvValueFromContainer(ctx, container, cp.Namespace, "CONTROLLER_ADMISSION_WEBHOOK_LISTEN", cl)
	if err != nil {
		log.Debug(logger, "unable to get CONTROLLER_ADMISSION_WEBHOOK_LISTEN env var", "error", err)
		return false
	}
	if !ok {
		return false
	}
	// We don't validate the value of the env var here, just that it is set.
	return len(admissionWebhookListen) > 0 && admissionWebhookListen != "off"
}
