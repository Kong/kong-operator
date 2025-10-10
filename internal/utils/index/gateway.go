package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// GatewayClassOnGatewayIndex is the name of the index that maps GatewayClass names to Gateways referencing them.
	GatewayClassOnGatewayIndex = "GatewayClassOnGateway"
)

// OptionsForGateway returns a slice of Option configured for indexing Gateway objects by GatewayClass name.
func OptionsForGateway() []Option {
	return []Option{
		{
			Object:         &gwtypes.Gateway{},
			Field:          GatewayClassOnGatewayIndex,
			ExtractValueFn: GatewayClassOnGateway,
		},
	}
}

// GatewayClassOnGateway extracts and returns the GatewayClass name referenced by the given Gateway object.
func GatewayClassOnGateway(o client.Object) []string {
	gateway, ok := o.(*gwtypes.Gateway)
	if !ok {
		return nil
	}
	if gateway.Spec.GatewayClassName == "" {
		return nil
	}
	return []string{string(gateway.Spec.GatewayClassName)}
}
