package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// BackendServicesOnUDPRouteIndex is the name of the index that maps Services to UDPRoutes
	// referencing them in their backendRefs.
	BackendServicesOnUDPRouteIndex = "BackendServicesOnUDPRoute"

	// GatewayOnUDPRouteIndex is the name of the index that maps Gateways to UDPRoutes referencing them in their ParentRefs.
	GatewayOnUDPRouteIndex = "GatewayOnUDPRoute"
)

// OptionsForUDPRoute returns a slice of Option configured for indexing UDPRoute objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForUDPRoute() []Option {
	return []Option{
		{
			Object:         &gwtypes.UDPRoute{},
			Field:          BackendServicesOnUDPRouteIndex,
			ExtractValueFn: BackendServicesOnUDPRoute,
		},
		{
			Object:         &gwtypes.UDPRoute{},
			Field:          GatewayOnUDPRouteIndex,
			ExtractValueFn: GatewaysOnRoute[gwtypes.UDPRoute],
		},
	}
}

// BackendServicesOnUDPRoute extracts and returns a list of unique Service references (in "namespace/name" format)
// from the BackendRefs of the given UDPRoute object.
func BackendServicesOnUDPRoute(o client.Object) []string {
	udpRoute, ok := o.(*gwtypes.UDPRoute)
	if !ok {
		return nil
	}

	var services []string
	for _, rule := range udpRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if serviceKey, ok := backendRefToServiceKey(backendRef, udpRoute.Namespace); ok {
				services = append(services, serviceKey)
			}
		}
	}
	return lo.Uniq(services)
}
