package finalizers

import (
	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// HybridHTTPRouteFinalizer is the finalizer added to HTTPRoute objects to manage cleanup of generated resources.
	HybridHTTPRouteFinalizer = "gateway-operator.konghq.com/hybrid-httproute-cleanup"

	// HybridGatewayFinalizer is the finalizer added to Gateway objects to manage cleanup of generated resources.
	HybridGatewayFinalizer = "gateway-operator.konghq.com/hybrid-gateway-cleanup"

	// HybridDefaultFinalizer is the default finalizer for resources that don't have a specific finalizer defined.
	HybridDefaultFinalizer = "gateway-operator.konghq.com/hybrid-resource-cleanup"
)

// GetFinalizerForType returns the appropriate finalizer name for the given resource type.
// This function uses type switching to determine which finalizer constant to return.
func GetFinalizerForType[t converter.RootObject](obj t) string {
	switch any(obj).(type) {
	case gwtypes.HTTPRoute:
		return HybridHTTPRouteFinalizer
	case gwtypes.Gateway:
		return HybridGatewayFinalizer
	default:
		return HybridDefaultFinalizer
	}
}
