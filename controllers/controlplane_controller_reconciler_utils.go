package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/hashicorp/go-multierror"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

// numReplicasWhenNoDataplane represents the desired number of replicas
// for the controlplane deployment when no dataplane is set.
const numReplicasWhenNoDataplane = 0

// -----------------------------------------------------------------------------
// ControlPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *ControlPlaneReconciler) ensureIsMarkedScheduled(
	controlplane *operatorv1alpha1.ControlPlane,
) bool {
	_, present := k8sutils.GetCondition(ControlPlaneConditionTypeProvisioned, controlplane)
	if !present {
		condition := k8sutils.NewCondition(
			ControlPlaneConditionTypeProvisioned,
			metav1.ConditionFalse,
			ControlPlaneConditionReasonPodsNotReady,
			"ControlPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, controlplane)
		return true
	}

	return false
}

func (r *ControlPlaneReconciler) ensureIsMarkedProvisioned(
	controlplane *operatorv1alpha1.ControlPlane,
) {
	condition := k8sutils.NewCondition(
		ControlPlaneConditionTypeProvisioned,
		metav1.ConditionTrue,
		ControlPlaneConditionReasonPodsReady,
		"pods for all Deployments are ready",
	)
	k8sutils.SetCondition(condition, controlplane)
	k8sutils.SetReady(controlplane, controlplane.Generation)
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane deployments reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *ControlPlaneReconciler) ensureDataPlaneStatus(
	controlplane *operatorv1alpha1.ControlPlane,
) (dataplaneIsSet bool) {
	dataplaneIsSet = controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane != ""
	condition, present := k8sutils.GetCondition(ControlPlaneConditionTypeProvisioned, controlplane)

	newCondition := k8sutils.NewCondition(
		ControlPlaneConditionTypeProvisioned,
		metav1.ConditionFalse,
		ControlPlaneConditionReasonNoDataplane,
		"DataPlane is not set",
	)
	if dataplaneIsSet {
		newCondition = k8sutils.NewCondition(
			ControlPlaneConditionTypeProvisioned,
			metav1.ConditionFalse,
			ControlPlaneConditionReasonPodsNotReady,
			"DataPlane was set, ControlPlane resource is scheduled for provisioning",
		)
	}
	if !present || condition.Status != newCondition.Status || condition.Reason != newCondition.Reason {
		k8sutils.SetCondition(newCondition, controlplane)
	}
	return dataplaneIsSet
}

// -----------------------------------------------------------------------------
// ControlPlaneReconciler - Spec Management
// -----------------------------------------------------------------------------

func (r *ControlPlaneReconciler) ensureDataPlaneConfiguration(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
	dataplaneServiceName string,
) error {
	changed := setControlPlaneEnvOnDataPlaneChange(
		&controlplane.Spec.ControlPlaneDeploymentOptions,
		controlplane.Namespace,
		dataplaneServiceName,
	)
	if changed {
		return r.Client.Update(ctx, controlplane)
	}
	return nil
}

// -----------------------------------------------------------------------------
// ControlPlaneReconciler - Owned Resource Management
// -----------------------------------------------------------------------------

// ensureDeploymentForControlPlane ensures that a Deployment is created for the
// ControlPlane resource. Deployment will remain in dormant state until
// corresponding dataplane is set.
func (r *ControlPlaneReconciler) ensureDeploymentForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
	serviceAccountName, certSecretName string,
) (bool, *appsv1.Deployment, error) {
	dataplaneIsSet := controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane != ""

	deployments, err := k8sutils.ListDeploymentsForOwner(ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.ControlPlaneManagedLabelValue,
		controlplane.Namespace,
		controlplane.UID,
	)
	if err != nil {
		return false, nil, err
	}

	count := len(deployments)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d deployments for ControlPlane currently unsupported: expected 1 or less", count)
	}

	generatedDeployment := generateNewDeploymentForControlPlane(controlplane, serviceAccountName, certSecretName)
	k8sutils.SetOwnerForObject(generatedDeployment, controlplane)
	addLabelForControlPlane(generatedDeployment)

	if count == 1 {
		var updated bool
		existingDeployment := &deployments[0]
		updated, existingDeployment.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingDeployment.ObjectMeta, generatedDeployment.ObjectMeta)
		container := k8sutils.GetPodContainerByName(&existingDeployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
		if container == nil {
			// someone has deleted the main container from the Deployment for ??? reasons. we can't fathom why they
			// would do this, but don't allow it and replace the container set entirely
			existingDeployment.Spec.Template.Spec.Containers = generatedDeployment.Spec.Template.Spec.Containers
			updated = true
			container = k8sutils.GetPodContainerByName(&existingDeployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
		}

		replicas := existingDeployment.Spec.Replicas
		switch {

		// Dataplane was just unset, so we need to scale down the Deployment.
		case !dataplaneIsSet && (replicas == nil || *replicas != numReplicasWhenNoDataplane):
			existingDeployment.Spec.Replicas = pointer.Int32(numReplicasWhenNoDataplane)
			updated = true

		// Dataplane was just set, so we need to scale up the Deployment
		// and ensure the env variables that might have been changed in
		// deployment are updated.
		case dataplaneIsSet && (replicas != nil && *replicas == numReplicasWhenNoDataplane):
			existingDeployment.Spec.Replicas = nil
			if len(container.Env) > 0 {
				container.Env = controlplane.Spec.Env
			}
			updated = true
		}

		// update cluster certificate volumes if needed.
		if r.deploymentSpecVolumesNeedsUpdate(&generatedDeployment.Spec, &existingDeployment.Spec) {
			existingDeployment.Spec.Template.Spec.Volumes = generatedDeployment.Spec.Template.Spec.Volumes
			updated = true
		}

		// update service account name if needed.
		if generatedDeployment.Spec.Template.Spec.ServiceAccountName !=
			existingDeployment.Spec.Template.Spec.ServiceAccountName {
			existingDeployment.Spec.Template.Spec.ServiceAccountName = generatedDeployment.Spec.Template.Spec.ServiceAccountName
			updated = true
		}

		// We do not want to permit direct edits of the Deployment environment. Any user-supplied values should be set
		// in the ControlPlane. If the actual Deployment environment does not match the generated environment, either
		// something requires an update (e.g. the associated DataPlane Service changed and value generation changed the
		// publish service configuration) or there was a manual edit we want to purge.
		if !reflect.DeepEqual(container.Env, controlplane.Spec.Env) {
			container.Env = controlplane.Spec.Env
			updated = true
		}

		if !reflect.DeepEqual(container.EnvFrom, controlplane.Spec.EnvFrom) {
			container.EnvFrom = controlplane.Spec.EnvFrom
			updated = true
		}

		// update the container image or image version if needed
		imageUpdated, err := ensureContainerImageUpdated(container, controlplane.Spec.ContainerImage, controlplane.Spec.Version)
		if err != nil {
			return false, nil, err
		}
		if imageUpdated {
			updated = true
		}

		if updated {
			return true, existingDeployment, r.Client.Update(ctx, existingDeployment)
		}

		return false, existingDeployment, nil
	}

	if !dataplaneIsSet {
		generatedDeployment.Spec.Replicas = pointer.Int32(numReplicasWhenNoDataplane)
	}
	return true, generatedDeployment, r.Client.Create(ctx, generatedDeployment)
}

func (r *ControlPlaneReconciler) ensureServiceAccountForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (createdOrModified bool, sa *corev1.ServiceAccount, err error) {
	serviceAccounts, err := k8sutils.ListServiceAccountsForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.Namespace, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(serviceAccounts)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d serviceAccounts for ControlPlane currently unsupported: expected 1 or less", count)
	}

	generatedServiceAccount := k8sresources.GenerateNewServiceAccountForControlPlane(controlplane.Namespace, controlplane.Name)
	k8sutils.SetOwnerForObject(generatedServiceAccount, controlplane)
	addLabelForControlPlane(generatedServiceAccount)

	if count == 1 {
		var updated bool
		existingServiceAccount := &serviceAccounts[0]
		updated, existingServiceAccount.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingServiceAccount.ObjectMeta, generatedServiceAccount.ObjectMeta)
		if updated {
			return true, existingServiceAccount, r.Client.Update(ctx, existingServiceAccount)
		}
		return false, existingServiceAccount, nil
	}

	return true, generatedServiceAccount, r.Client.Create(ctx, generatedServiceAccount)
}

func (r *ControlPlaneReconciler) ensureClusterRoleForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (createdOrUpdated bool, cr *rbacv1.ClusterRole, err error) {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoles)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d ClusterRoles for ControlPlane currently unsupported: expected 1 or less", count)
	}

	generatedClusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane(controlplane.Name, controlplane.Spec.ContainerImage)
	if err != nil {
		return false, nil, err
	}
	k8sutils.SetOwnerForObject(generatedClusterRole, controlplane)
	addLabelForControlPlane(generatedClusterRole)

	if count == 1 {
		var updated bool
		existingClusterRole := &clusterRoles[0]
		updated, existingClusterRole.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingClusterRole.ObjectMeta, generatedClusterRole.ObjectMeta)
		if updated {
			return true, existingClusterRole, r.Client.Update(ctx, existingClusterRole)
		}
		return false, existingClusterRole, nil
	}

	return true, generatedClusterRole, r.Client.Create(ctx, generatedClusterRole)
}

func (r *ControlPlaneReconciler) ensureClusterRoleBindingForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
	serviceAccountName string,
	clusterRoleName string,
) (createdOrUpdate bool, crb *rbacv1.ClusterRoleBinding, err error) {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoleBindings)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d ClusterRoleBindings for ControlPlane currently unsupported: expected 1 or less", count)
	}

	generatedClusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForControlPlane(controlplane.Namespace, controlplane.Name, serviceAccountName, clusterRoleName)
	k8sutils.SetOwnerForObject(generatedClusterRoleBinding, controlplane)
	addLabelForControlPlane(generatedClusterRoleBinding)

	if count == 1 {
		var updated bool
		existingClusterRoleBinding := &clusterRoleBindings[0]
		updated, existingClusterRoleBinding.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingClusterRoleBinding.ObjectMeta, generatedClusterRoleBinding.ObjectMeta)
		if updated {
			return true, existingClusterRoleBinding, r.Client.Update(ctx, existingClusterRoleBinding)
		}
		return false, existingClusterRoleBinding, nil
	}

	return true, generatedClusterRoleBinding, r.Client.Create(ctx, generatedClusterRoleBinding)
}

func (r *ControlPlaneReconciler) ensureCertificate(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (bool, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature, certificatesv1.UsageClientAuth,
	}
	// this subject is arbitrary. data planes only care that client certificates are signed by the trusted CA, and will
	// accept a certificate with any subject
	return maybeCreateCertificateSecret(ctx,
		controlplane,
		fmt.Sprintf("%s.%s", controlplane.Name, controlplane.Namespace),
		r.ClusterCASecretName,
		r.ClusterCASecretNamespace,
		usages,
		r.Client)
}

// ensureOwnedClusterRolesDeleted removes all the owned ClusterRoles of the controlplane.
// it is called on cleanup of owned cluster resources on controlplane deletion.
// returns nil if all of owned ClusterRoles successfully deleted (ok if no owned CRs or NotFound on deleting CRs).
func (r *ControlPlaneReconciler) ensureOwnedClusterRolesDeleted(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (deletions bool, err error) {
	var deleted bool
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx, r.Client,
		consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID,
	)
	if err != nil {
		return false, err
	}

	var deletionErr *multierror.Error
	for i := range clusterRoles {
		err = r.Client.Delete(ctx, &clusterRoles[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			deletionErr = multierror.Append(deletionErr, err)
		}
		deleted = true
	}

	return deleted, deletionErr.ErrorOrNil()

}

// ensureOwnedClusterRoleBindingsDeleted removes all the owned ClusterRoleBindings of the controlplane
// it is called on cleanup of owned cluster resources on controlplane deletion.
// returns nil if all of owned ClusterRoleBindings successfully deleted (ok if no owned CRBs or NotFound on deleting CRBs).
func (r *ControlPlaneReconciler) ensureOwnedClusterRoleBindingsDeleted(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (deletions bool, err error) {
	var deleted bool
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx, r.Client,
		consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID,
	)
	if err != nil {
		return false, err
	}

	var deletionErr *multierror.Error
	for i := range clusterRoleBindings {
		err = r.Client.Delete(ctx, &clusterRoleBindings[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			deletionErr = multierror.Append(deletionErr, err)
		}
		deleted = true
	}

	return deleted, deletionErr.ErrorOrNil()
}

// deploymentSpecVolumesNeedsUpdate returns true if the volumes in deployment
// for controlplane needs to be updated.
func (r *ControlPlaneReconciler) deploymentSpecVolumesNeedsUpdate(
	generatedDeploymentSpec *appsv1.DeploymentSpec,
	existingDeploymentSpec *appsv1.DeploymentSpec,
) bool {
	generatedClusterCertVolume := k8sutils.GetPodVolumeByName(&generatedDeploymentSpec.Template.Spec, consts.ClusterCertificateVolume)
	existingClusterCertVolume := k8sutils.GetPodVolumeByName(&existingDeploymentSpec.Template.Spec, consts.ClusterCertificateVolume)
	// check for cluster certificate volume.
	if generatedClusterCertVolume == nil || existingClusterCertVolume == nil {
		return true
	}

	if generatedClusterCertVolume.Secret == nil || existingClusterCertVolume.Secret == nil {
		return true
	}

	if generatedClusterCertVolume.Secret.SecretName != existingClusterCertVolume.Secret.SecretName {
		return true
	}

	return false
}
