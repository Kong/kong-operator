package kubernetes

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Public Functions - Owner References
// -----------------------------------------------------------------------------

// ListDataPlanesForGateway is a helper function to map a list of DataPlanes
// that are owned and managed by a Gateway.
func ListDataPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gatewayv1alpha2.Gateway,
) ([]operatorv1alpha1.DataPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	requirement, err := labels.NewRequirement(
		consts.GatewayOperatorControlledLabel,
		selection.Equals,
		[]string{consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		Namespace:     gateway.Namespace,
		LabelSelector: selector,
	}

	dataplaneList := &operatorv1alpha1.DataPlaneList{}
	if err := c.List(ctx, dataplaneList, listOptions); err != nil {
		return nil, err
	}

	dataplanes := make([]operatorv1alpha1.DataPlane, 0)
	for _, dataplane := range dataplaneList.Items {
		for _, ownerRef := range dataplane.ObjectMeta.OwnerReferences {
			if ownerRef.UID == gateway.UID {
				dataplanes = append(dataplanes, dataplane)
				break
			}
		}
	}

	return dataplanes, nil
}

// ListControlPlanesForGateway is a helper function to map a list of ControlPlanes
// that are owned and managed by a Gateway.
func ListControlPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gatewayv1alpha2.Gateway,
) ([]operatorv1alpha1.ControlPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	requirement, err := labels.NewRequirement(
		consts.GatewayOperatorControlledLabel,
		selection.Equals,
		[]string{consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		Namespace:     gateway.Namespace,
		LabelSelector: selector,
	}

	controlplaneList := &operatorv1alpha1.ControlPlaneList{}
	if err := c.List(ctx, controlplaneList, listOptions); err != nil {
		return nil, err
	}

	controlplanes := make([]operatorv1alpha1.ControlPlane, 0)
	for _, controlplane := range controlplaneList.Items {
		for _, ownerRef := range controlplane.ObjectMeta.OwnerReferences {
			if ownerRef.UID == gateway.UID {
				controlplanes = append(controlplanes, controlplane)
				break
			}
		}
	}

	return controlplanes, nil
}

// ListDeploymentsForOwner is a helper function to map a list of Deployments
// by label and reduce by OwnerReference UID and namespace to efficiently list
// only the objects owned by the provided UID.
func ListDeploymentsForOwner(
	ctx context.Context,
	c client.Client,
	requiredLabel string,
	requiredValue string,
	namespace string,
	uid types.UID,
) ([]appsv1.Deployment, error) {
	requirement, err := labels.NewRequirement(
		requiredLabel,
		selection.Equals,
		[]string{requiredValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		LabelSelector: selector,
	}
	if namespace != "" {
		listOptions.Namespace = namespace
	}

	deploymentList := &appsv1.DeploymentList{}
	if err := c.List(ctx, deploymentList, listOptions); err != nil {
		return nil, err
	}

	deployments := make([]appsv1.Deployment, 0)
	for _, deployment := range deploymentList.Items {
		for _, ownerRef := range deployment.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				deployments = append(deployments, deployment)
				break
			}
		}
	}

	return deployments, nil
}

// ListServicesForOwner is a helper function to map a list of Services
// by label and reduce by OwnerReference UID and namespace to efficiently list
// only the objects owned by the provided UID.
func ListServicesForOwner(
	ctx context.Context,
	c client.Client,
	requiredLabel string,
	requiredValue string,
	namespace string,
	uid types.UID,
) ([]corev1.Service, error) {
	requirement, err := labels.NewRequirement(
		requiredLabel,
		selection.Equals,
		[]string{requiredValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		LabelSelector: selector,
	}
	if namespace != "" {
		listOptions.Namespace = namespace
	}

	serviceList := &corev1.ServiceList{}
	if err := c.List(ctx, serviceList, listOptions); err != nil {
		return nil, err
	}

	services := make([]corev1.Service, 0)
	for _, service := range serviceList.Items {
		for _, ownerRef := range service.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				services = append(services, service)
				break
			}
		}
	}

	return services, nil
}
