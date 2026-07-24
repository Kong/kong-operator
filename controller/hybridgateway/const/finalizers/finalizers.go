package finalizers

import (
	"github.com/kong/kong-operator/v2/controller/hybridgateway/converter"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// HybridHTTPRouteFinalizer is the finalizer added to HTTPRoute objects to manage cleanup of generated resources.
	HybridHTTPRouteFinalizer = "gateway-operator.konghq.com/hybrid-httproute-cleanup"

	// HybridTLSRouteFinalizer is the finalizer added to TLSRoute objects to manage cleanup of generated resources.
	HybridTLSRouteFinalizer = "gateway-operator.konghq.com/hybrid-tlsroute-cleanup"

	// HybridTCPRouteFinalizer is the finalizer added to TCPRoute objects to manage cleanup of generated resources.
	HybridTCPRouteFinalizer = "gateway-operator.konghq.com/hybrid-tcproute-cleanup"

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
	case gwtypes.TLSRoute:
		return HybridTLSRouteFinalizer
	case gwtypes.TCPRoute:
		return HybridTCPRouteFinalizer
	case gwtypes.Gateway:
		return HybridGatewayFinalizer
	default:
		return HybridDefaultFinalizer
	}
}
