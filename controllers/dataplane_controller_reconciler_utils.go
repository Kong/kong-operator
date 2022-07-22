package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedScheduled(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, error) {
	isScheduled := false
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			isScheduled = true
		}
	}

	if !isScheduled {
		dataplane.Status.Conditions = append(dataplane.Status.Conditions, metav1.Condition{
			Type:               string(DataPlaneConditionTypeProvisioned),
			Reason:             DataPlaneConditionReasonPodsNotReady,
			Status:             metav1.ConditionFalse,
			Message:            "DataPlane resource is scheduled for provisioning",
			ObservedGeneration: dataplane.Generation,
			LastTransitionTime: metav1.Now(),
		})
		return true, r.Client.Status().Update(ctx, dataplane)
	}

	return false, nil
}

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedProvisioned(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) error {
	updatedConditions := make([]metav1.Condition, 0)
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			condition.Status = metav1.ConditionTrue
			condition.Reason = DataPlaneConditionReasonPodsReady
			condition.Message = "pods for all Deployments are ready"
			condition.ObservedGeneration = dataplane.Generation
			condition.LastTransitionTime = metav1.Now()
		}
		updatedConditions = append(updatedConditions, condition)
	}
	dataplane.Status.Conditions = updatedConditions
	return r.Status().Update(ctx, dataplane)
}

// isSameDataPlaneCondition returns true if two `metav1.Condition`s
// indicates the same condition of a `DataPlane` resource.
func isSameDataPlaneCondition(condition1, condition2 metav1.Condition) bool {
	return condition1.Type == condition2.Type &&
		condition1.Status == condition2.Status &&
		condition1.Reason == condition2.Reason &&
		condition1.Message == condition2.Message
}

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedNotProvisioned(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
	reason string, message string,
) error {
	notProvisionedCondition := metav1.Condition{
		Type:               string(DataPlaneConditionTypeProvisioned),
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dataplane.Generation,
		LastTransitionTime: metav1.Now(),
	}

	conditionFound := false
	shouldUpdate := false
	for i, condition := range dataplane.Status.Conditions {
		// update the condition if condition has type `provisioned`, and the condition is not the same.
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			conditionFound = true
			// update the slice if the condition is not the same as we expected.
			if !isSameDataPlaneCondition(notProvisionedCondition, condition) {
				dataplane.Status.Conditions[i] = notProvisionedCondition
				shouldUpdate = true
			}
		}
	}

	if !conditionFound {
		// append a new condition if provisioned condition is not found.
		dataplane.Status.Conditions = append(dataplane.Status.Conditions, notProvisionedCondition)
		shouldUpdate = true
	}

	if shouldUpdate {
		return r.Status().Update(ctx, dataplane)
	}
	return nil
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Owned Resource Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureDeploymentForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, *appsv1.Deployment, error) {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return false, nil, err
	}

	count := len(deployments)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d deployments for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &deployments[0], nil
	}

	deployment := generateNewDeploymentForDataPlane(dataplane)
	k8sutils.SetOwnerForObject(deployment, dataplane)
	labelObjForDataplane(deployment)
	return true, deployment, r.Client.Create(ctx, deployment)
}

func (r *DataPlaneReconciler) ensureServiceForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, *corev1.Service, error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return false, nil, err
	}

	count := len(services)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d services for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &services[0], nil
	}

	service := generateNewServiceForDataplane(dataplane)
	labelObjForDataplane(service)
	return true, service, r.Client.Create(ctx, service)
}
