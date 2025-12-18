package finalizers

import (
	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// HTTPRouteFinalizer is the finalizer added to HTTPRoute objects to manage cleanup of generated resources.
	HTTPRouteFinalizer = "gateway-operator.konghq.com/httproute-cleanup"

	// GatewayFinalizer is the finalizer added to Gateway objects to manage cleanup of generated resources.
	GatewayFinalizer = "gateway-operator.konghq.com/gateway-cleanup"

	// DefaultFinalizer is the default finalizer for resources that don't have a specific finalizer defined.
	DefaultFinalizer = "gateway-operator.konghq.com/resource-cleanup"
)

// GetFinalizerForType returns the appropriate finalizer name for the given resource type.
// This function uses type switching to determine which finalizer constant to return.
func GetFinalizerForType[t converter.RootObject](obj t) string {
	switch any(obj).(type) {
	case gwtypes.HTTPRoute:
		return HTTPRouteFinalizer
	case gwtypes.Gateway:
		return GatewayFinalizer
	default:
		return DefaultFinalizer
	}
}
