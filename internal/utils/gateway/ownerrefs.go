package gateway

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Public Functions - Owner References
// -----------------------------------------------------------------------------

// ListDataPlanesForGateway is a helper function to map a list of DataPlanes
// that are owned and managed by a Gateway.
func ListDataPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
) ([]operatorv1beta1.DataPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	dataplaneList := &operatorv1beta1.DataPlaneList{}

	err := c.List(
		ctx,
		dataplaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	dataplanes := make([]operatorv1beta1.DataPlane, 0)
	for _, dataplane := range dataplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&dataplane.ObjectMeta, gateway.UID) {
			dataplanes = append(dataplanes, dataplane)
		}
	}

	return dataplanes, nil
}

// ListControlPlanesForGateway is a helper function to map a list of ControlPlanes
// that are owned and managed by a Gateway.
func ListControlPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
) ([]operatorv1alpha1.ControlPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	controlplaneList := &operatorv1alpha1.ControlPlaneList{}

	err := c.List(
		ctx,
		controlplaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	controlplanes := make([]operatorv1alpha1.ControlPlane, 0)
	for _, controlplane := range controlplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&controlplane.ObjectMeta, gateway.UID) {
			controlplanes = append(controlplanes, controlplane)
		}
	}

	return controlplanes, nil
}

// GetDataPlaneForControlPlane retrieves the DataPlane object referenced by a ControlPlane
func GetDataPlaneForControlPlane(
	ctx context.Context,
	c client.Client,
	controlplane *operatorv1alpha1.ControlPlane,
) (*operatorv1beta1.DataPlane, error) {
	if controlplane.Spec.DataPlane == nil || *controlplane.Spec.DataPlane == "" {
		return nil, fmt.Errorf("%w, controlplane = %s/%s", operatorerrors.ErrDataPlaneNotSet, controlplane.Namespace, controlplane.Name)
	}

	dataplane := operatorv1beta1.DataPlane{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: controlplane.Namespace, Name: *controlplane.Spec.DataPlane}, &dataplane); err != nil {
		return nil, err
	}
	return &dataplane, nil
}

// GetDataplaneServiceName is a helper function that retrieves the name of the service owned by provided dataplane.
// It accepts a string as the last argument to specify which service to retrieve (proxy/admin)
func GetDataplaneServiceName(
	ctx context.Context,
	c client.Client,
	dataplane *operatorv1beta1.DataPlane,
	serviceTypeLabelValue consts.ServiceType,
) (string, error) {
	services, err := k8sutils.ListServicesForOwner(ctx,
		c,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:      string(serviceTypeLabelValue),
		},
	)
	if err != nil {
		return "", err
	}

	count := len(services)
	if count > 1 {
		return "", fmt.Errorf("found %d %s services for DataPlane currently unsupported: expected 1 or less", count, serviceTypeLabelValue)
	}

	if count == 0 {
		return "", fmt.Errorf("found 0 %s services for DataPlane", serviceTypeLabelValue)
	}

	return services[0].Name, nil
}

// ListNetworkPoliciesForGateway is a helper function that returns a list of NetworkPolicies
// that are owned and managed by a Gateway.
func ListNetworkPoliciesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
) ([]networkingv1.NetworkPolicy, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list networkpolicies for gateway: gateway resource was missing namespace")
	}

	networkPolicyList := &networkingv1.NetworkPolicyList{}

	err := c.List(
		ctx,
		networkPolicyList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{consts.GatewayOperatorControlledLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	networkPolicies := make([]networkingv1.NetworkPolicy, 0)
	for _, networkPolicy := range networkPolicyList.Items {
		if k8sutils.IsOwnedByRefUID(&networkPolicy.ObjectMeta, gateway.UID) {
			networkPolicies = append(networkPolicies, networkPolicy)
		}
	}

	return networkPolicies, nil
}
