package gateway

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
		client.MatchingLabels{consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	dataplanes := make([]operatorv1beta1.DataPlane, 0)
	for _, dataplane := range dataplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&dataplane, gateway.UID) {
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
) ([]gwtypes.ControlPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list dataplanes for gateway: gateway resource was missing namespace")
	}

	controlplaneList := &gwtypes.ControlPlaneList{}

	err := c.List(
		ctx,
		controlplaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
		},
	)
	if err != nil {
		return nil, err
	}

	controlplanes := make([]gwtypes.ControlPlane, 0)
	for _, controlplane := range controlplaneList.Items {
		if k8sutils.IsOwnedByRefUID(&controlplane, gateway.UID) {
			controlplanes = append(controlplanes, controlplane)
		}
	}

	return controlplanes, nil
}

// ListKonnectGatewayControlPlanesForGateway is a helper function to map a list of KonnectGatewayControlPlanes
// that are owned and managed by a Gateway.
func ListKonnectGatewayControlPlanesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
) ([]konnectv1alpha2.KonnectGatewayControlPlane, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list KonnectGatewayControlPlanes for gateway: gateway resource was missing namespace")
	}

	konnectControlPlaneList := &konnectv1alpha2.KonnectGatewayControlPlaneList{}

	err := c.List(
		ctx,
		konnectControlPlaneList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
		},
	)
	if err != nil {
		return nil, err
	}

	konnectControlPlanes := make([]konnectv1alpha2.KonnectGatewayControlPlane, 0)
	for _, konnectControlPlane := range konnectControlPlaneList.Items {
		if k8sutils.IsOwnedByRefUID(&konnectControlPlane, gateway.UID) {
			konnectControlPlanes = append(konnectControlPlanes, konnectControlPlane)
		}
	}

	return konnectControlPlanes, nil
}

// ListKonnectExtensionsForGateway is a helper function to map a list of KonnectExtensions
// that are owned and managed by a Gateway.
func ListKonnectExtensionsForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
) ([]konnectv1alpha2.KonnectExtension, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list KonnectExtensions for gateway: gateway resource was missing namespace")
	}

	konnectExtensionList := &konnectv1alpha2.KonnectExtensionList{}

	err := c.List(
		ctx,
		konnectExtensionList,
		client.InNamespace(gateway.Namespace),
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue,
		},
	)
	if err != nil {
		return nil, err
	}

	konnectExtensions := make([]konnectv1alpha2.KonnectExtension, 0)
	for _, konnectExtension := range konnectExtensionList.Items {
		if k8sutils.IsOwnedByRefUID(&konnectExtension, gateway.UID) {
			konnectExtensions = append(konnectExtensions, konnectExtension)
		}
	}

	return konnectExtensions, nil
}

// ListHTTPRoutesForGateway is a helper function which returns a list of HTTPRoutes
// that have the provided Gateway set as parent in their status.
func ListHTTPRoutesForGateway(
	ctx context.Context,
	c client.Client,
	gateway *gwtypes.Gateway,
	opts ...client.ListOption,
) ([]gwtypes.HTTPRoute, error) {
	if gateway.Namespace == "" {
		return nil, fmt.Errorf("can't list HTTPRoutes for gateway: Gateway %s was missing namespace", gateway.Name)
	}

	var httpRoutesList gwtypes.HTTPRouteList
	err := c.List(
		ctx,
		&httpRoutesList,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("can't list HTTPRoutes for gateway: %w", err)
	}

	var httpRoutes []gwtypes.HTTPRoute
	for _, httpRoute := range httpRoutesList.Items {
		if !lo.ContainsBy(httpRoute.Spec.ParentRefs, func(parentRef gwtypes.ParentReference) bool {
			gwGVK := gateway.GroupVersionKind()
			if parentRef.Group != nil && string(*parentRef.Group) != gwGVK.Group {
				return false
			}
			if parentRef.Kind != nil && string(*parentRef.Kind) != gwGVK.Kind {
				return false
			}
			if string(parentRef.Name) != gateway.Name {
				return false
			}

			if parentRef.SectionName != nil {
				if !lo.ContainsBy(gateway.Spec.Listeners, func(listener gwtypes.Listener) bool {
					if listener.Name != *parentRef.SectionName {
						return false
					}
					if parentRef.Port != nil && listener.Port != *parentRef.Port {
						return false
					}
					return true
				}) {
					return false
				}
			}

			return true
		}) {
			continue
		}

		httpRoutes = append(httpRoutes, httpRoute)
	}

	return httpRoutes, nil
}

// GetDataPlaneServiceName is a helper function that retrieves the name of the service owned by provided dataplane.
// It accepts a string as the last argument to specify which service to retrieve (proxy/admin)
func GetDataPlaneServiceName(
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
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(serviceTypeLabelValue),
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
		client.MatchingLabels{consts.GatewayOperatorManagedByLabel: consts.GatewayManagedLabelValue},
	)
	if err != nil {
		return nil, err
	}

	networkPolicies := make([]networkingv1.NetworkPolicy, 0)
	for _, networkPolicy := range networkPolicyList.Items {
		if k8sutils.IsOwnedByRefUID(&networkPolicy, gateway.UID) {
			networkPolicies = append(networkPolicies, networkPolicy)
		}
	}

	return networkPolicies, nil
}
