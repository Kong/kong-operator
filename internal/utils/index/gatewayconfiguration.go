package index

import operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

// OptionsForGatewayConfiguration returns the options for GatewayConfiguration.
func OptionsForGatewayConfiguration() []Option {
	return []Option{
		{
			Object:         &operatorv1beta1.GatewayConfiguration{},
			Field:          KonnectExtensionIndex,
			ExtractValueFn: extendableOnKonnectExtension[*operatorv1beta1.GatewayConfiguration](),
		},
	}
}
