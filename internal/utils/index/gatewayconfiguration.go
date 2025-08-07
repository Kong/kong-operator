package index

import (
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
)

// OptionsForGatewayConfiguration returns the options for GatewayConfiguration.
func OptionsForGatewayConfiguration() []Option {
	return []Option{
		{
			Object:         &operatorv2beta1.GatewayConfiguration{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*operatorv2beta1.GatewayConfiguration](),
		},
	}
}
