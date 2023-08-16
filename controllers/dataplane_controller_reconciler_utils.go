package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureIsMarkedScheduled(
	dataplane *operatorv1beta1.DataPlane,
) bool {
	_, present := k8sutils.GetCondition(DataPlaneConditionTypeProvisioned, dataplane)
	if !present {
		condition := k8sutils.NewConditionWithGeneration(
			DataPlaneConditionTypeProvisioned,
			metav1.ConditionFalse,
			DataPlaneConditionReasonPodsNotReady,
			"DataPlane resource is scheduled for provisioning",
			dataplane.Generation,
		)

		k8sutils.SetCondition(condition, dataplane)
		return true
	}
	return false
}

// ensureReadinessStatus ensures the readiness Status fields of DataPlane are set.
func ensureReadinessStatus(
	dataplane *operatorv1beta1.DataPlane,
	dataplaneDeployment *appsv1.Deployment,
) {
	readyCond, ok := k8sutils.GetCondition(k8sutils.ReadyType, dataplane)
	dataplane.Status.Ready = ok && readyCond.Status == metav1.ConditionTrue

	dataplane.Status.Replicas = dataplaneDeployment.Status.Replicas
	dataplane.Status.ReadyReplicas = dataplaneDeployment.Status.ReadyReplicas
}

func addressOf[T any](v T) *T {
	return &v
}

func (r *DataPlaneReconciler) ensureDataPlaneServiceStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	dataplaneServiceName string,
) (bool, error) {
	shouldUpdate := false
	if dataplane.Status.Service != dataplaneServiceName {
		dataplane.Status.Service = dataplaneServiceName
		shouldUpdate = true
	}

	if shouldUpdate {
		return true, r.patchStatus(ctx, log, dataplane)
	}
	return false, nil
}

// ensureDataPlaneAddressesStatus ensures that provided DataPlane's status addresses
// are as expected and pathes its status if there's a difference between the
// current state and what's expected.
// It returns a boolean indicating if the patch has been trigerred and an error.
func (r *DataPlaneReconciler) ensureDataPlaneAddressesStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	dataplaneService *corev1.Service,
) (bool, error) {
	addresses, err := addressesFromService(dataplaneService)
	if err != nil {
		return false, fmt.Errorf("failed getting addresses for service %s: %w", dataplaneService, err)
	}

	// Compare the lengths prior to cmp.Equal() because cmp.Equal() will return
	// false when comparing nil slice and 0 length slice.
	if len(addresses) != len(dataplane.Status.Addresses) ||
		!cmp.Equal(addresses, dataplane.Status.Addresses) {
		dataplane.Status.Addresses = addresses
		return true, r.patchStatus(ctx, log, dataplane)
	}

	return false, nil
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
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	reason k8sutils.ConditionReason, message string,
) error {
	notProvisionedCondition := metav1.Condition{
		Type:               string(DataPlaneConditionTypeProvisioned),
		Status:             metav1.ConditionFalse,
		Reason:             string(reason),
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
		return r.patchStatus(ctx, log, dataplane)
	}
	return nil
}

// ensureDataPlaneIngressServiceAnnotationsUpdated updates annotations of existing ingress service
// owned by the `DataPlane`. It first removes outdated annotations and then update annotations
// in current spec of `DataPlane`.
func ensureDataPlaneIngressServiceAnnotationsUpdated(
	dataplane *operatorv1beta1.DataPlane, existingAnnotations map[string]string, generatedAnnotations map[string]string,
) (bool, map[string]string, error) {
	// Remove annotations applied from previous version of DataPlane but removed in the current version.
	// Should be done before updating new annotations, because the updating process will overwrite the annotation
	// to save last applied annotations.
	outdatedAnnotations, err := extractOutdatedDataPlaneIngressServiceAnnotations(dataplane, existingAnnotations)
	if err != nil {
		return true, existingAnnotations, fmt.Errorf("failed to extract outdated annotations: %w", err)
	}
	var shouldUpdate bool
	for k := range outdatedAnnotations {
		if _, ok := existingAnnotations[k]; ok {
			delete(existingAnnotations, k)
			shouldUpdate = true
		}
	}
	if generatedAnnotations != nil && existingAnnotations == nil {
		existingAnnotations = map[string]string{}
	}
	// set annotations by current specified ingress service annotations.
	for k, v := range generatedAnnotations {
		if existingAnnotations[k] != v {
			existingAnnotations[k] = v
			shouldUpdate = true
		}
	}
	return shouldUpdate, existingAnnotations, nil
}

// dataPlaneIngressServiceIsReady returns:
//   - true for DataPlanes that do not have the Ingress Service type set as LoadBalancer
//   - true for DataPlanes that have the Ingress Service type set as LoadBalancer and
//     which have at least one IP or Hostname in their Ingress Service Status
//   - false otherwise.
func dataPlaneIngressServiceIsReady(dataplane *operatorv1beta1.DataPlane, dataplaneIngressService *corev1.Service) bool {
	// If the DataPlane doesn't have a LoadBalancer set for its Ingress Service
	// return true.
	if dataplane.Spec.Network.Services == nil ||
		dataplane.Spec.Network.Services.Ingress == nil ||
		dataplane.Spec.Network.Services.Ingress.Type != corev1.ServiceTypeLoadBalancer {
		return true
	}

	ingressStatuses := dataplaneIngressService.Status.LoadBalancer.Ingress
	// If there are ingress statuses attached to the ingress Service, check
	// if there are IPs of Hostnames specified.
	// If that's the case, the DataPlane is Ready.
	for _, ingressStatus := range ingressStatuses {
		if ingressStatus.Hostname != "" || ingressStatus.IP != "" {
			return true
		}
	}
	// Otherwise the DataPlane is not Ready.
	return false
}
