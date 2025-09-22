package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
)

const (
	// GatewayClassOnGatewayConfigurationIndex is the key to index the GatewayClass
	// based on the referenced GatewayConfiguration.
	GatewayClassOnGatewayConfigurationIndex = "GatewayClassOnGatewayConfiguration"
)

func OptionsForGatewayClass() []Option {
	return []Option{
		{
			Object:         &gatewayv1.GatewayClass{},
			Field:          GatewayClassOnGatewayConfigurationIndex,
			ExtractValueFn: GatewayConfigurationOnGatewayClass,
		},
	}
}

func GatewayConfigurationOnGatewayClass(o client.Object) []string {
	gwc, ok := o.(*gatewayv1.GatewayClass)
	if !ok {
		return nil
	}

	params := gwc.Spec.ParametersRef
	if params == nil {
		return nil
	}

	if string(params.Group) != operatorv2beta1.SchemeGroupVersion.Group ||
		params.Kind != "GatewayConfiguration" {
		return nil
	}

	if params.Namespace == nil {
		return nil
	}

	return []string{string(*params.Namespace) + "/" + params.Name}
}
