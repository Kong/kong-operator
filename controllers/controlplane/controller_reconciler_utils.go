package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/semver"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/controlplane"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	"github.com/kong/gateway-operator/controllers/pkg/patch"
	"github.com/kong/gateway-operator/controllers/pkg/secrets"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

// numReplicasWhenNoDataPlane represents the desired number of replicas
// for the controlplane deployment when no dataplane is set.
const numReplicasWhenNoDataPlane = 0

// -----------------------------------------------------------------------------
// Reconciler - Status Management
// -----------------------------------------------------------------------------

func (r *Reconciler) ensureIsMarkedScheduled(
	controlplane *operatorv1beta1.ControlPlane,
) bool {
	_, present := k8sutils.GetCondition(ConditionTypeProvisioned, controlplane)
	if !present {
		condition := k8sutils.NewCondition(
			ConditionTypeProvisioned,
			metav1.ConditionFalse,
			ConditionReasonPodsNotReady,
			"ControlPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, controlplane)
		return true
	}

	return false
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane deployments reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *Reconciler) ensureDataPlaneStatus(
	controlplane *operatorv1beta1.ControlPlane,
	dataplane *operatorv1beta1.DataPlane,
) (dataplaneIsSet bool) {
	dataplaneIsSet = controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane == dataplane.Name
	condition, present := k8sutils.GetCondition(ConditionTypeProvisioned, controlplane)

	newCondition := k8sutils.NewCondition(
		ConditionTypeProvisioned,
		metav1.ConditionFalse,
		ConditionReasonNoDataPlane,
		"DataPlane is not set",
	)
	if dataplaneIsSet {
		newCondition = k8sutils.NewCondition(
			ConditionTypeProvisioned,
			metav1.ConditionFalse,
			ConditionReasonPodsNotReady,
			"DataPlane was set, ControlPlane resource is scheduled for provisioning",
		)
	}
	if !present || condition.Status != newCondition.Status || condition.Reason != newCondition.Reason {
		k8sutils.SetCondition(newCondition, controlplane)
	}
	return dataplaneIsSet
}

// -----------------------------------------------------------------------------
// Reconciler - Spec Management
// -----------------------------------------------------------------------------

func (r *Reconciler) ensureDataPlaneConfiguration(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
	dataplaneServiceName string,
) error {
	changed := setControlPlaneEnvOnDataPlaneChange(
		&controlplane.Spec.ControlPlaneOptions,
		controlplane.Namespace,
		dataplaneServiceName,
	)
	if changed {
		if err := r.Client.Update(ctx, controlplane); err != nil {
			return fmt.Errorf("failed updating ControlPlane's DataPlane: %w", err)
		}
		return nil
	}
	return nil
}

func setControlPlaneEnvOnDataPlaneChange(
	spec *operatorv1beta1.ControlPlaneOptions,
	namespace string,
	dataplaneServiceName string,
) bool {
	var changed bool

	dataplaneIsSet := spec.DataPlane != nil && *spec.DataPlane != ""
	container := k8sutils.GetPodContainerByName(&spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if dataplaneIsSet {
		newPublishServiceValue := k8stypes.NamespacedName{Namespace: namespace, Name: dataplaneServiceName}.String()
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_PUBLISH_SERVICE") != newPublishServiceValue {
			container.Env = k8sutils.UpdateEnv(container.Env, "CONTROLLER_PUBLISH_SERVICE", newPublishServiceValue)
			changed = true
		}
	} else {
		if k8sutils.EnvValueByName(container.Env, "CONTROLLER_PUBLISH_SERVICE") != "" {
			container.Env = k8sutils.RejectEnvByName(container.Env, "CONTROLLER_PUBLISH_SERVICE")
			changed = true
		}
	}

	return changed
}

// -----------------------------------------------------------------------------
// Reconciler - Owned Resource Management
// -----------------------------------------------------------------------------

// ensureDeploymentParams is a helper struct to pass parameters to the ensureDeployment method.
type ensureDeploymentParams struct {
	ControlPlane                   *operatorv1beta1.ControlPlane
	ServiceAccountName             string
	AdminMTLSCertSecretName        string
	AdmissionWebhookCertSecretName string
}

// ensureDeployment ensures that a Deployment is created for the
// ControlPlane resource. Deployment will remain in dormant state until
// corresponding dataplane is set.
func (r *Reconciler) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	params ensureDeploymentParams,
) (op.CreatedUpdatedOrNoop, *appsv1.Deployment, error) {
	dataplaneIsSet := params.ControlPlane.Spec.DataPlane != nil && *params.ControlPlane.Spec.DataPlane != ""

	deployments, err := k8sutils.ListDeploymentsForOwner(ctx,
		r.Client,
		params.ControlPlane.Namespace,
		params.ControlPlane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return op.Noop, nil, err
	}

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, r.Client, deployments); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of deployments reduced")
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !r.DevelopmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneImageVersionSupported)
	}
	controlplaneImage, err := controlplane.GenerateImage(&params.ControlPlane.Spec.ControlPlaneOptions, versionValidationOptions...)
	if err != nil {
		return op.Noop, nil, err
	}
	generatedDeployment, err := k8sresources.GenerateNewDeploymentForControlPlane(k8sresources.GenerateNewDeploymentForControlPlaneParams{
		ControlPlane:                   params.ControlPlane,
		ControlPlaneImage:              controlplaneImage,
		ServiceAccountName:             params.ServiceAccountName,
		AdminMTLSCertSecretName:        params.AdminMTLSCertSecretName,
		AdmissionWebhookCertSecretName: params.AdmissionWebhookCertSecretName,
	})
	if err != nil {
		return op.Noop, nil, err
	}

	if count == 1 {
		var updated bool
		existingDeployment := &deployments[0]
		oldExistingDeployment := existingDeployment.DeepCopy()

		// ensure that object metadata is up to date
		updated, existingDeployment.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingDeployment.ObjectMeta, generatedDeployment.ObjectMeta)

		// some custom comparison rules are needed for some PodTemplateSpec sub-attributes, in particular
		// resources and affinity.
		opts := []cmp.Option{
			cmp.Comparer(func(a, b corev1.ResourceRequirements) bool { return k8sresources.ResourceRequirementsEqual(a, b) }),
		}

		// ensure that PodTemplateSpec is up to date
		if !cmp.Equal(existingDeployment.Spec.Template, generatedDeployment.Spec.Template, opts...) {
			existingDeployment.Spec.Template = generatedDeployment.Spec.Template
			updated = true
		}

		// ensure that replication strategy is up to date
		replicas := params.ControlPlane.Spec.ControlPlaneOptions.Deployment.Replicas
		switch {
		case !dataplaneIsSet && (replicas == nil || *replicas != numReplicasWhenNoDataPlane):
			// DataPlane was just unset, so we need to scale down the Deployment.
			if !cmp.Equal(existingDeployment.Spec.Replicas, lo.ToPtr(int32(numReplicasWhenNoDataPlane))) {
				existingDeployment.Spec.Replicas = lo.ToPtr(int32(numReplicasWhenNoDataPlane))
				updated = true
			}
		case dataplaneIsSet && (replicas != nil && *replicas != numReplicasWhenNoDataPlane):
			// DataPlane was just set, so we need to scale up the Deployment
			// and ensure the env variables that might have been changed in
			// deployment are updated.
			if !cmp.Equal(existingDeployment.Spec.Replicas, replicas) {
				existingDeployment.Spec.Replicas = replicas
				updated = true
			}
		}

		return patch.ApplyPatchIfNonEmpty(ctx, r.Client, logger, existingDeployment, oldExistingDeployment, params.ControlPlane, updated)
	}

	if !dataplaneIsSet {
		generatedDeployment.Spec.Replicas = lo.ToPtr(int32(numReplicasWhenNoDataPlane))
	}
	if err := r.Client.Create(ctx, generatedDeployment); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating ControlPlane Deployment %s: %w", generatedDeployment.Name, err)
	}

	log.Debug(logger, "deployment for ControlPlane created", params.ControlPlane, "deployment", generatedDeployment.Name)
	return op.Created, generatedDeployment, nil
}

func (r *Reconciler) ensureServiceAccount(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
) (createdOrModified bool, sa *corev1.ServiceAccount, err error) {
	serviceAccounts, err := k8sutils.ListServiceAccountsForOwner(
		ctx,
		r.Client,
		controlplane.Namespace,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(serviceAccounts)
	if count > 1 {
		if err := k8sreduce.ReduceServiceAccounts(ctx, r.Client, serviceAccounts); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of serviceAccounts reduced")
	}

	generatedServiceAccount := k8sresources.GenerateNewServiceAccountForControlPlane(controlplane.Namespace, controlplane.Name)
	k8sutils.SetOwnerForObject(generatedServiceAccount, controlplane)

	if count == 1 {
		var updated bool
		existingServiceAccount := &serviceAccounts[0]
		updated, existingServiceAccount.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingServiceAccount.ObjectMeta, generatedServiceAccount.ObjectMeta)
		if updated {
			if err := r.Client.Update(ctx, existingServiceAccount); err != nil {
				return false, existingServiceAccount, fmt.Errorf("failed updating ControlPlane's ServiceAccount %s: %w", existingServiceAccount.Name, err)
			}
			return true, existingServiceAccount, nil
		}
		return false, existingServiceAccount, nil
	}

	return true, generatedServiceAccount, r.Client.Create(ctx, generatedServiceAccount)
}

func (r *Reconciler) ensureClusterRole(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
) (createdOrUpdated bool, cr *rbacv1.ClusterRole, err error) {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx,
		r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoles)
	if count > 1 {
		if err := k8sreduce.ReduceClusterRoles(ctx, r.Client, clusterRoles); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of clusterRoles reduced")
	}

	controlplaneContainer := k8sutils.GetPodContainerByName(&controlplane.Spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	generatedClusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane(controlplane.Name, controlplaneContainer.Image, r.DevelopmentMode)
	if err != nil {
		return false, nil, err
	}
	k8sutils.SetOwnerForObject(generatedClusterRole, controlplane)

	if count == 1 {
		var updated bool
		existingClusterRole := &clusterRoles[0]
		updated, generatedClusterRole.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingClusterRole.ObjectMeta, generatedClusterRole.ObjectMeta)
		if updated ||
			!cmp.Equal(existingClusterRole.Rules, generatedClusterRole.Rules) ||
			!cmp.Equal(existingClusterRole.AggregationRule, generatedClusterRole.AggregationRule) {
			if err := r.Client.Patch(ctx, generatedClusterRole, client.MergeFrom(existingClusterRole)); err != nil {
				return false, existingClusterRole, fmt.Errorf("failed patching ControlPlane's ClusterRole %s: %w", existingClusterRole.Name, err)
			}
			return true, generatedClusterRole, nil
		}
		return false, generatedClusterRole, nil
	}

	return true, generatedClusterRole, r.Client.Create(ctx, generatedClusterRole)
}

func (r *Reconciler) ensureClusterRoleBinding(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
	serviceAccountName string,
	clusterRoleName string,
) (createdOrUpdate bool, crb *rbacv1.ClusterRoleBinding, err error) {
	logger := log.GetLogger(ctx, "controlplane.ensureClusterRoleBinding", r.DevelopmentMode)

	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx,
		r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoleBindings)
	if count > 1 {
		if err := k8sreduce.ReduceClusterRoleBindings(ctx, r.Client, clusterRoleBindings); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of clusterRoleBindings reduced")
	}

	generatedClusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForControlPlane(controlplane.Namespace, controlplane.Name, serviceAccountName, clusterRoleName)
	k8sutils.SetOwnerForObject(generatedClusterRoleBinding, controlplane)

	if count == 1 {
		existingClusterRoleBinding := &clusterRoleBindings[0]
		// Delete and re-create ClusterRoleBinding if name of ClusterRole changed because RoleRef is immutable.
		if !k8sresources.CompareClusterRoleName(existingClusterRoleBinding, clusterRoleName) {
			log.Debug(logger, "ClusterRole name changed, delete and re-create a ClusterRoleBinding",
				existingClusterRoleBinding,
				"old_cluster_role", existingClusterRoleBinding.RoleRef.Name,
				"new_cluster_role", clusterRoleName,
			)
			if err := r.Client.Delete(ctx, existingClusterRoleBinding); err != nil {
				return false, nil, err
			}
			return false, nil, errors.New("name of ClusterRole changed, out of date ClusterRoleBinding deleted")
		}
		var updated bool
		updated, generatedClusterRoleBinding.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingClusterRoleBinding.ObjectMeta, generatedClusterRoleBinding.ObjectMeta)
		if updated ||
			!k8sresources.ClusterRoleBindingContainsServiceAccount(existingClusterRoleBinding, controlplane.Namespace, serviceAccountName) {
			if err := r.Client.Patch(ctx, generatedClusterRoleBinding, client.MergeFrom(existingClusterRoleBinding)); err != nil {
				return false, existingClusterRoleBinding, fmt.Errorf("failed patching ControlPlane's ClusterRoleBinding %s: %w", existingClusterRoleBinding.Name, err)
			}
			return true, generatedClusterRoleBinding, nil
		}
		return false, generatedClusterRoleBinding, nil

	}

	return true, generatedClusterRoleBinding, r.Client.Create(ctx, generatedClusterRoleBinding)
}

// ensureAdminMTLSCertificateSecret ensures that a Secret is created with the certificate for mTLS communication between the
// ControlPlane and the DataPlane.
func (r *Reconciler) ensureAdminMTLSCertificateSecret(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
) (
	op.CreatedUpdatedOrNoop,
	*corev1.Secret,
	error,
) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretUsedByServiceLabel: consts.ControlPlaneServiceKindAdmin,
	}
	// this subject is arbitrary. data planes only care that client certificates are signed by the trusted CA, and will
	// accept a certificate with any subject
	return secrets.EnsureCertificate(ctx,
		controlplane,
		fmt.Sprintf("%s.%s", controlplane.Name, controlplane.Namespace),
		k8stypes.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.Client,
		matchingLabels,
	)
}

// ensureAdmissionWebhookCertificateSecret ensures that a Secret is created with the serving certificate for the
// ControlPlane's admission webhook.
func (r *Reconciler) ensureAdmissionWebhookCertificateSecret(
	ctx context.Context,
	cp *operatorv1beta1.ControlPlane,
	admissionWebhookService *corev1.Service,
) (
	op.CreatedUpdatedOrNoop,
	*corev1.Secret,
	error,
) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageServerAuth,
		certificatesv1.UsageDigitalSignature,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretUsedByServiceLabel: consts.ControlPlaneServiceKindWebhook,
	}
	return secrets.EnsureCertificate(ctx,
		cp,
		fmt.Sprintf("%s.%s.svc", admissionWebhookService.Name, admissionWebhookService.Namespace),
		k8stypes.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.Client,
		matchingLabels,
	)
}

// ensureOwnedClusterRolesDeleted removes all the owned ClusterRoles of the controlplane.
// it is called on cleanup of owned cluster resources on controlplane deletion.
// returns nil if all of owned ClusterRoles successfully deleted (ok if no owned CRs or NotFound on deleting CRs).
func (r *Reconciler) ensureOwnedClusterRolesDeleted(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
) (deletions bool, err error) {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx, r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range clusterRoles {
		err = r.Client.Delete(ctx, &clusterRoles[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

// ensureOwnedClusterRoleBindingsDeleted removes all the owned ClusterRoleBindings of the controlplane
// it is called on cleanup of owned cluster resources on controlplane deletion.
// returns nil if all of owned ClusterRoleBindings successfully deleted (ok if no owned CRBs or NotFound on deleting CRBs).
func (r *Reconciler) ensureOwnedClusterRoleBindingsDeleted(
	ctx context.Context,
	controlplane *operatorv1beta1.ControlPlane,
) (deletions bool, err error) {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx, r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range clusterRoleBindings {
		err = r.Client.Delete(ctx, &clusterRoleBindings[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedValidatingWebhookConfigurationDeleted(ctx context.Context, cp *operatorv1beta1.ControlPlane) (deletions bool, err error) {
	validatingWebhookConfigurations, err := k8sutils.ListValidatingWebhookConfigurationsForOwner(
		ctx,
		r.Client,
		cp.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, fmt.Errorf("failed listing webhook configurations for owner: %w", err)
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range validatingWebhookConfigurations {
		err = r.Client.Delete(ctx, &validatingWebhookConfigurations[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}
	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureAdmissionWebhookService(
	ctx context.Context,
	cl client.Client,
	controlPlane *operatorv1beta1.ControlPlane,
) (op.CreatedUpdatedOrNoop, *corev1.Service, error) {
	matchingLabels := k8sresources.GetManagedLabelForOwner(controlPlane)
	matchingLabels[consts.ControlPlaneServiceLabel] = consts.ControlPlaneServiceKindWebhook

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		controlPlane.Namespace,
		controlPlane.UID,
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing admission webhook Services for ControlPlane %s/%s: %w", controlPlane.Namespace, controlPlane.Name, err)
	}

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, cl, services); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of ControlPlane admission webhook Services reduced")
	}

	generatedService, err := k8sresources.GenerateNewAdmissionWebhookServiceForControlPlane(controlPlane)
	if err != nil {
		return op.Noop, nil, err
	}

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta)

		if !cmp.Equal(existingService.Spec.Selector, generatedService.Spec.Selector) {
			existingService.Spec.Selector = generatedService.Spec.Selector
			updated = true
		}
		if !cmp.Equal(existingService.Spec.Ports, generatedService.Spec.Ports) {
			existingService.Spec.Ports = generatedService.Spec.Ports
			updated = true
		}

		if updated {
			if err := cl.Update(ctx, existingService); err != nil {
				return op.Noop, existingService, fmt.Errorf("failed updating ControlPlane admission webhook Service %s: %w", existingService.Name, err)
			}
			return op.Updated, existingService, nil
		}
		return op.Noop, existingService, nil
	}

	if err := cl.Create(ctx, generatedService); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating ControlPlane admission webhook Service: %w", err)
	}

	return op.Created, generatedService, nil
}

func (r *Reconciler) ensureValidatingWebhookConfiguration(
	ctx context.Context,
	cp *operatorv1beta1.ControlPlane,
	certSecret *corev1.Secret,
	webhookServiceName string,
) (op.CreatedUpdatedOrNoop, error) {
	logger := log.GetLogger(ctx, "controlplane.ensureValidatingWebhookConfiguration", r.DevelopmentMode)

	validatingWebhookConfigurations, err := k8sutils.ListValidatingWebhookConfigurationsForOwner(
		ctx,
		r.Client,
		cp.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return op.Noop, err
	}

	count := len(validatingWebhookConfigurations)
	if count > 1 {
		if err := k8sreduce.ReduceValidatingWebhookConfigurations(ctx, r.Client, validatingWebhookConfigurations); err != nil {
			return op.Noop, err
		}
		return op.Noop, errors.New("number of validatingWebhookConfigurations reduced")
	}

	cpContainer := k8sutils.GetPodContainerByName(&cp.Spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if cpContainer == nil {
		return op.Noop, errors.New("controller container not found")
	}
	cpVersion, err := imageToSemverVersion(cpContainer.Image)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to parse control plane image version: %w", err)
	}

	caBundle, ok := certSecret.Data["ca.crt"]
	if !ok {
		return op.Noop, errors.New("ca.crt not found in secret")
	}
	generatedWebhookConfiguration, err := k8sresources.GenerateValidatingWebhookConfigurationForControlPlane(
		cp.Name,
		cpVersion,
		admregv1.WebhookClientConfig{
			Service: &admregv1.ServiceReference{
				Namespace: cp.Namespace,
				Name:      webhookServiceName,
				Port:      lo.ToPtr(int32(consts.ControlPlaneAdmissionWebhookListenPort)),
			},
			CABundle: caBundle,
		},
	)
	if err != nil {
		return op.Noop, fmt.Errorf("failed generating ControlPlane's ValidatingWebhookConfiguration: %w", err)
	}
	k8sutils.SetOwnerForObject(generatedWebhookConfiguration, cp)

	if count == 1 {
		var updated bool
		webhookConfiguration := validatingWebhookConfigurations[0]
		oldWebhookConfiguration := webhookConfiguration.DeepCopy()

		updated, generatedWebhookConfiguration.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(webhookConfiguration.ObjectMeta, generatedWebhookConfiguration.ObjectMeta)
		if !cmp.Equal(webhookConfiguration.Webhooks, generatedWebhookConfiguration.Webhooks) {
			webhookConfiguration.Webhooks = generatedWebhookConfiguration.Webhooks
			updated = true
		}

		if updated {
			log.Debug(logger, "patching existing ValidatingWebhookConfiguration", webhookConfiguration)
			return op.Updated, r.Client.Patch(ctx, &webhookConfiguration, client.MergeFrom(oldWebhookConfiguration))
		}

		return op.Noop, nil
	}

	return op.Created, r.Client.Create(ctx, generatedWebhookConfiguration)
}

func imageToSemverVersion(image string) (*semver.Version, error) {
	// First, parse the image to get the version string.
	v, err := versions.FromImage(image)
	if err != nil {
		return nil, fmt.Errorf("failed to parse control plane image version: %w", err)
	}
	// Construct a semver version from the version string.
	return semver.NewVersion(v.String())
}
