package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/versions"
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
	dataplane *operatorv1alpha1.DataPlane,
) (dataplaneIsSet bool) {
	dataplaneIsSet = controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane == dataplane.Name
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
		controlplane.Namespace,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, r.Client, deployments); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of deployments reduced")
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !r.DevelopmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneImageVersionSupported)
	}
	controlplaneImage, err := generateControlPlaneImage(&controlplane.Spec.ControlPlaneOptions, versionValidationOptions...)
	if err != nil {
		return false, nil, err
	}
	generatedDeployment, err := k8sresources.GenerateNewDeploymentForControlPlane(controlplane, controlplaneImage, serviceAccountName, certSecretName)
	if err != nil {
		return false, nil, err
	}

	k8sutils.SetOwnerForObject(generatedDeployment, controlplane)
	addLabelForControlPlane(generatedDeployment)

	if count == 1 {
		var updated bool
		existingDeployment := &deployments[0]

		// ensure that object metadata is up to date
		updated, existingDeployment.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingDeployment.ObjectMeta, generatedDeployment.ObjectMeta)

		// some custom comparison rules are needed for some PodTemplateSpec sub-attributes, in particular
		// resources and affinity.
		opts := []cmp.Option{
			cmp.Comparer(func(a, b corev1.ResourceRequirements) bool { return k8sresources.ResourceRequirementsEqual(a, b) }),
		}

		// ensure that PodTemplateSpec is up to date
		// TODO: this is currently relying on us pre-empting API server defaults (by setting them ourselves).
		// This could in theory lead to situations of incompatibility with newer Kubernetes versions down the
		// road, the tradeoff was made due to a time crunch of a matter of hours. We should consider other options
		// to verify whether there are changes staged.
		//
		// See: https://github.com/Kong/gateway-operator/issues/904
		if !cmp.Equal(existingDeployment.Spec.Template, generatedDeployment.Spec.Template, opts...) {
			existingDeployment.Spec.Template = generatedDeployment.Spec.Template
			updated = true
		}

		// ensure that replication strategy is up to date
		replicas := controlplane.Spec.ControlPlaneOptions.Deployment.Replicas
		switch {
		case !dataplaneIsSet && (replicas == nil || *replicas != numReplicasWhenNoDataplane):
			// Dataplane was just unset, so we need to scale down the Deployment.
			if !cmp.Equal(existingDeployment.Spec.Replicas, pointer.Int32(numReplicasWhenNoDataplane)) {
				existingDeployment.Spec.Replicas = pointer.Int32(numReplicasWhenNoDataplane)
				updated = true
			}
		case dataplaneIsSet && (replicas != nil && *replicas != numReplicasWhenNoDataplane):
			// Dataplane was just set, so we need to scale up the Deployment
			// and ensure the env variables that might have been changed in
			// deployment are updated.
			if !cmp.Equal(existingDeployment.Spec.Replicas, replicas) {
				existingDeployment.Spec.Replicas = replicas
				updated = true
			}
		}

		if updated {
			if err := r.Client.Update(ctx, existingDeployment); err != nil {
				return true, existingDeployment, fmt.Errorf("failed updating ControlPlane's Deployment %s: %w", existingDeployment.Name, err)
			}
			return true, existingDeployment, nil
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
	serviceAccounts, err := k8sutils.ListServiceAccountsForOwner(
		ctx,
		r.Client,
		controlplane.Namespace,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
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
	addLabelForControlPlane(generatedServiceAccount)

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

func (r *ControlPlaneReconciler) ensureClusterRoleForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (createdOrUpdated bool, cr *rbacv1.ClusterRole, err error) {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx,
		r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
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
	generatedClusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane(controlplane.Name, controlplaneContainer.Image)
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
			if err := r.Client.Update(ctx, existingClusterRole); err != nil {
				return false, existingClusterRole, fmt.Errorf("failed updating ControlPlane's ClusterRole %s: %w", existingClusterRole.Name, err)
			}
			return true, existingClusterRole, nil
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
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx,
		r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
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
	addLabelForControlPlane(generatedClusterRoleBinding)

	if count == 1 {
		var updated bool
		existingClusterRoleBinding := &clusterRoleBindings[0]
		updated, existingClusterRoleBinding.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingClusterRoleBinding.ObjectMeta, generatedClusterRoleBinding.ObjectMeta)
		if updated {
			if err := r.Client.Update(ctx, existingClusterRoleBinding); err != nil {
				return true, existingClusterRoleBinding, fmt.Errorf("failed updating ControlPlane's ClusterRoleBinding %s: %w", existingClusterRoleBinding.Name, err)
			}
			return true, existingClusterRoleBinding, nil
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
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
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
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(
		ctx, r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
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
func (r *ControlPlaneReconciler) ensureOwnedClusterRoleBindingsDeleted(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (deletions bool, err error) {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(
		ctx, r.Client,
		controlplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.ControlPlaneManagedLabelValue,
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

// getDataPlanePod returns the IP of the newest DataPlane pod.
func getDataPlanePod(ctx context.Context, cl client.Reader, dataplaneName, namespace string) (*corev1.Pod, error) {
	podList := corev1.PodList{}
	if err := cl.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{
		"app": dataplaneName,
	}); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, operatorerrors.ErrNoDataPlanePods
	}
	newestDataPlanePod := podList.Items[0]
	for _, pod := range podList.Items[1:] {
		if pod.DeletionTimestamp != nil || pod.Status.PodIP == "" {
			continue
		}
		if pod.CreationTimestamp.After(newestDataPlanePod.CreationTimestamp.Time) {
			newestDataPlanePod = pod
		}
	}
	return &newestDataPlanePod, nil
}
