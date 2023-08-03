package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureIsMarkedScheduled(
	dataplane *operatorv1beta1.DataPlane,
) bool {
	_, present := k8sutils.GetCondition(DataPlaneConditionTypeProvisioned, dataplane)
	if !present {
		condition := k8sutils.NewCondition(
			DataPlaneConditionTypeProvisioned,
			metav1.ConditionFalse,
			DataPlaneConditionReasonPodsNotReady,
			"DataPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, dataplane)
		return true
	}
	return false
}

func (r *DataPlaneReconciler) ensureIsMarkedProvisioned(
	dataplane *operatorv1beta1.DataPlane,
) {
	condition := k8sutils.NewCondition(
		DataPlaneConditionTypeProvisioned,
		metav1.ConditionTrue,
		DataPlaneConditionReasonPodsReady,
		"pods for all Deployments are ready",
	)
	k8sutils.SetCondition(condition, dataplane)
	k8sutils.SetReady(dataplane, dataplane.Generation)
}

func (r *DataPlaneReconciler) ensureReadinessStatus(
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
	if dataplane.Status.Service != dataplaneServiceName {
		dataplane.Status.Service = dataplaneServiceName
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

func (r *DataPlaneReconciler) ensureProxyServiceForDataPlane(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) (createdOrUpdated bool, svc *corev1.Service, err error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneProxyServiceLabelValue),
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, r.Client, services); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of dataplane proxy services reduced")
	}

	generatedService, err := k8sresources.GenerateNewProxyServiceForDataplane(dataplane)
	if err != nil {
		return false, nil, err
	}
	addLabelForDataplane(generatedService)
	addAnnotationsForDataplaneProxyService(generatedService, *dataplane)
	k8sutils.SetOwnerForObject(generatedService, dataplane)

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta,
			// enforce all the annotations provided through the dataplane API
			func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta) {
				var metaToUpdate bool
				if existingMeta.Annotations == nil && generatedMeta.Annotations != nil {
					existingMeta.Annotations = map[string]string{}
				}
				for k, v := range generatedMeta.Annotations {
					if existingMeta.Annotations[k] != v {
						existingMeta.Annotations[k] = v
						metaToUpdate = true
					}
				}
				return metaToUpdate, existingMeta
			})

		if existingService.Spec.Type != generatedService.Spec.Type {
			existingService.Spec.Type = generatedService.Spec.Type
			updated = true
		}
		if !cmp.Equal(existingService.Spec.Selector, generatedService.Spec.Selector) {
			existingService.Spec.Selector = generatedService.Spec.Selector
			updated = true
		}

		if updated {
			if err := r.Client.Update(ctx, existingService); err != nil {
				return false, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return true, existingService, nil
		}
		return false, existingService, nil
	}

	return true, generatedService, r.Client.Create(ctx, generatedService)
}

// dataPlaneProxyServiceIsReady returns:
//   - true for DataPlanes that do not have the Ingress Service type set as LoadBalancer
//   - true for DataPlanes that have the Ingress Service type set as LoadBalancer and
//     which have at least one IP or Hostname in their Ingress Service Status
//   - false otherwise.
func dataPlaneProxyServiceIsReady(dataplane *operatorv1beta1.DataPlane, dataplaneProxyService *corev1.Service) bool {
	// If the DataPlane doesn't have a LoadBalancer set for its Ingress Service
	// return true.
	if dataplane.Spec.Network.Services == nil ||
		dataplane.Spec.Network.Services.Ingress == nil ||
		dataplane.Spec.Network.Services.Ingress.Type != corev1.ServiceTypeLoadBalancer {
		return true
	}

	ingressStatuses := dataplaneProxyService.Status.LoadBalancer.Ingress
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
