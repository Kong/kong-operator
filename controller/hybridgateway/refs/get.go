package refs

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// GetGatewaysByHTTPRoute returns Gateways referenced by the given HTTPRoute.
func GetGatewaysByHTTPRoute(ctx context.Context, cl client.Client, r gwtypes.HTTPRoute) []gwtypes.Gateway {
	gatewayRefs := []gwtypes.Gateway{}
	for _, ref := range r.Spec.ParentRefs {
		var namespace string
		if ref.Group == nil || *ref.Group != "gateway.networking.k8s.io" {
			continue
		}
		if ref.Kind == nil || *ref.Kind != "Gateway" {
			continue
		}
		if ref.Namespace != nil && *ref.Namespace != "" {
			namespace = string(*ref.Namespace)
		} else {
			namespace = r.Namespace
		}
		gw := &gwtypes.Gateway{}
		err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      string(ref.Name),
		}, gw)
		if err != nil {
			continue
		}
		gatewayRefs = append(gatewayRefs, *gw)
	}
	return gatewayRefs
}

// getGatewayClassByGateway retrieves the GatewayClass resource associated with the given Gateway, or returns nil if not found.
func getGatewayClassByGateway(ctx context.Context, cl client.Client, gw gwtypes.Gateway) *gwtypes.GatewayClass {
	gwClass := &gwtypes.GatewayClass{}
	err := cl.Get(ctx, client.ObjectKey{
		Name: string(gw.Spec.GatewayClassName),
	}, gwClass)
	if err != nil {
		return nil
	}
	return gwClass
}

// getGatewayConfigurationByGatewayClass retrieves a GatewayConfiguration referenced by the given GatewayClass, or returns nil if not found or invalid.
func getGatewayConfigurationByGatewayClass(ctx context.Context, cl client.Client, gwClass gwtypes.GatewayClass) *gwtypes.GatewayConfiguration {
	if gwClass.Spec.ParametersRef == nil ||
		gwClass.Spec.ParametersRef.Group != "gateway-operator.konghq.com" ||
		gwClass.Spec.ParametersRef.Kind != "GatewayConfiguration" ||
		gwClass.Spec.ParametersRef.Namespace == nil {
		return nil
	}

	gwConf := gwtypes.GatewayConfiguration{}
	err := cl.Get(ctx, client.ObjectKey{
		Name:      gwClass.Spec.ParametersRef.Name,
		Namespace: string(*gwClass.Spec.ParametersRef.Namespace),
	}, &gwConf)
	if err != nil {
		return nil
	}

	return &gwConf
}
