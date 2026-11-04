package builder

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	metadataTestRouteName      = "test-route"
	metadataTestRouteNamespace = "route-ns"
	metadataTestGatewayName    = "test-gateway"
	metadataTestGatewayNS      = "gateway-ns"
)

func newHTTPRouteMetadataFixture() *gwtypes.HTTPRoute {
	return &gwtypes.HTTPRoute{
		APIVersion: gwtypes.GroupVersion.String(),
		Kind:       "HTTPRoute",
		Name:       metadataTestRouteName,
		Namespace:  metadataTestRouteNamespace,
	}
}

func newParentRefMetadataFixture() *gwtypes.ParentReference {
	namespace := gwtypes.Namespace(metadataTestGatewayNS)
	return &gwtypes.ParentReference{
		Name:      gwtypes.ObjectName(metadataTestGatewayName),
		Namespace: &namespace,
	}
}

func newGatewayMetadataFixture() *gwtypes.Gateway {
	return &gwtypes.Gateway{
		APIVersion: gwtypes.GroupVersion.String(),
		Kind:       "Gateway",
		Name:       metadataTestGatewayName,
		Namespace:  metadataTestGatewayNS,
	}
}

func wantHTTPRouteLabels() map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel: "HTTPRoute",
	}
}

func wantHTTPRouteAnnotations() map[string]string {
	return map[string]string{
		consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "route-ns/test-route",
		consts.GatewayOperatorHybridGatewaysAnnotation:        "gateway-ns/test-gateway",
	}
}

func wantGatewayLabels() map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel: "Gateway",
	}
}

func wantGatewayListenerLabels(port string) map[string]string {
	labels := wantGatewayLabels()
	labels[consts.GatewayOperatorHybridListenerPortLabel] = port
	return labels
}

func wantGatewayAnnotations() map[string]string {
	return map[string]string{
		consts.GatewayOperatorHybridGatewaysAnnotation: "gateway-ns/test-gateway",
	}
}
