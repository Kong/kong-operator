package index

import (
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"
)

// OptionsForGatewayConfiguration returns the options for GatewayConfiguration.
func OptionsForGatewayConfiguration() []Option {
	return []Option{
		{
			Object:         &operatorv2alpha1.GatewayConfiguration{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*operatorv2alpha1.GatewayConfiguration](),
		},
	}
}
