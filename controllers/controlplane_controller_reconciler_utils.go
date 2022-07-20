package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

func (r *ControlPlaneReconciler) ensureControlPlaneIsMarkedScheduled(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (bool, error) {
	isScheduled := false
	for _, condition := range controlplane.Status.Conditions {
		if condition.Type == string(ControlPlaneConditionTypeProvisioned) {
			isScheduled = true
		}
	}

	if !isScheduled {
		controlplane.Status.Conditions = append(controlplane.Status.Conditions, metav1.Condition{
			Type:               string(ControlPlaneConditionTypeProvisioned),
			Reason:             ControlPlaneConditionReasonPodsNotReady,
			Status:             metav1.ConditionFalse,
			Message:            "ControlPlane resource is scheduled for provisioning",
			ObservedGeneration: controlplane.Generation,
			LastTransitionTime: metav1.Now(),
		})
		return true, r.Client.Status().Update(ctx, controlplane)
	}

	return false, nil
}

func (r *ControlPlaneReconciler) ensureControlPlaneIsMarkedProvisioned(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) error {
	updatedConditions := make([]metav1.Condition, 0)
	for _, condition := range controlplane.Status.Conditions {
		if condition.Type == string(ControlPlaneConditionTypeProvisioned) {
			condition.Status = metav1.ConditionTrue
			condition.Reason = ControlPlaneConditionReasonPodsReady
			condition.Message = "pods for all Deployments are ready"
			condition.ObservedGeneration = controlplane.Generation
			condition.LastTransitionTime = metav1.Now()
		}
		updatedConditions = append(updatedConditions, condition)
	}

	controlplane.Status.Conditions = updatedConditions
	return r.Status().Update(ctx, controlplane)
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane deployments reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *ControlPlaneReconciler) ensureDataPlaneStatus(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (controlPlaneChanged, dataplaneIsSet bool, err error) {
	updatedConditions := make([]metav1.Condition, 0)
	dataplaneIsSet = controlplane.Spec.DataPlane != nil && *controlplane.Spec.DataPlane != ""

	for _, condition := range controlplane.Status.Conditions {
		if condition.Type == string(ControlPlaneConditionTypeProvisioned) {
			switch {

			// Change state to NoDataplane if dataplane is not set.
			case !dataplaneIsSet && condition.Reason != ControlPlaneConditionReasonNoDataplane:
				condition = metav1.Condition{
					Type:               string(ControlPlaneConditionTypeProvisioned),
					Reason:             ControlPlaneConditionReasonNoDataplane,
					Status:             metav1.ConditionFalse,
					Message:            "DataPlane is not set",
					ObservedGeneration: controlplane.Generation,
					LastTransitionTime: metav1.Now(),
				}
				updatedConditions = append(updatedConditions, condition)

			// Change state from NoDataplane to PodsNotReady to start provisioning.
			case dataplaneIsSet && condition.Reason == ControlPlaneConditionReasonNoDataplane:
				condition = metav1.Condition{
					Type:               string(ControlPlaneConditionTypeProvisioned),
					Reason:             ControlPlaneConditionReasonPodsNotReady,
					Status:             metav1.ConditionFalse,
					Message:            "DataPlane was set, ControlPlane resource is scheduled for provisioning",
					ObservedGeneration: controlplane.Generation,
					LastTransitionTime: metav1.Now(),
				}
				updatedConditions = append(updatedConditions, condition)
			}
		}
	}

	if len(updatedConditions) > 0 {
		controlplane.Status.Conditions = updatedConditions
		return true, dataplaneIsSet, r.Status().Update(ctx, controlplane)
	}

	return false, dataplaneIsSet, nil
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
	serviceAccountName string,
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

	if count == 1 {
		replicas := deployments[0].Spec.Replicas

		switch {

		// Dataplane was just unset, so we need to scale down the Deployment.
		case !dataplaneIsSet && (replicas == nil || *replicas != numReplicasWhenNoDataplane):
			deployments[0].Spec.Replicas = pointer.Int32(numReplicasWhenNoDataplane)
			return true, &deployments[0], r.Client.Update(ctx, &deployments[0])

		// Dataplane was just set, so we need to scale up the Deployment
		// and ensure the env variables that might have been changed in
		// deployment are updated.
		case dataplaneIsSet && (replicas != nil && *replicas == numReplicasWhenNoDataplane):
			deployments[0].Spec.Replicas = nil
			if len(deployments[0].Spec.Template.Spec.Containers[0].Env) > 0 {
				deployments[0].Spec.Template.Spec.Containers[0].Env = controlplane.Spec.Env
			}
			return true, &deployments[0], r.Client.Update(ctx, &deployments[0])

		// Dataplane is unchanged, so we don't need to do anything.
		default:
			return false, &deployments[0], nil
		}
	}

	deployment := generateNewDeploymentForControlPlane(controlplane, serviceAccountName)
	k8sutils.SetOwnerForObject(deployment, controlplane)
	labelObjForControlPlane(deployment)

	if !dataplaneIsSet {
		deployment.Spec.Replicas = pointer.Int32(numReplicasWhenNoDataplane)
	}

	return true, deployment, r.Client.Create(ctx, deployment)
}

func (r *ControlPlaneReconciler) ensureServiceAccountForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (created bool, sa *corev1.ServiceAccount, err error) {
	serviceAccounts, err := k8sutils.ListServiceAccountsForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.Namespace, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(serviceAccounts)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d serviceAccounts for ControlPlane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &serviceAccounts[0], nil
	}

	serviceAccount := k8sresources.GenerateNewServiceAccountForControlPlane(controlplane.Namespace, controlplane.Name)
	k8sutils.SetOwnerForObject(serviceAccount, controlplane)
	labelObjForControlPlane(serviceAccount)
	return true, serviceAccount, r.Client.Create(ctx, serviceAccount)
}

func (r *ControlPlaneReconciler) ensureClusterRoleForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
) (created bool, cr *rbacv1.ClusterRole, err error) {
	clusterRoles, err := k8sutils.ListClusterRolesForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoles)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d deployments for ControlPlane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &clusterRoles[0], nil
	}

	clusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane(controlplane.Name, controlplane.Spec.ContainerImage)
	if err != nil {
		return false, nil, err
	}
	k8sutils.SetOwnerForObject(clusterRole, controlplane)
	labelObjForControlPlane(clusterRole)
	return true, clusterRole, r.Client.Create(ctx, clusterRole)
}

func (r *ControlPlaneReconciler) ensureClusterRoleBindingForControlPlane(
	ctx context.Context,
	controlplane *operatorv1alpha1.ControlPlane,
	serviceAccountName string,
	clusterRoleName string,
) (created bool, crb *rbacv1.ClusterRoleBinding, err error) {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindingsForOwner(ctx, r.Client, consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue, controlplane.UID)
	if err != nil {
		return false, nil, err
	}

	count := len(clusterRoleBindings)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d deployments for ControlPlane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &clusterRoleBindings[0], nil
	}

	clusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForControlPlane(controlplane.Namespace, controlplane.Name, serviceAccountName, clusterRoleName)
	k8sutils.SetOwnerForObject(clusterRoleBinding, controlplane)
	labelObjForControlPlane(clusterRoleBinding)
	return true, clusterRoleBinding, r.Client.Create(ctx, clusterRoleBinding)
}
