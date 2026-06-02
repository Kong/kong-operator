package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// BackendServicesOnTLSRouteIndex is the name of the index that maps Services to TLSRoutes
	// referencing them in their backendRefs.
	BackendServicesOnTLSRouteIndex = "BackendServicesOnTLSRoute"

	// GatewayOnTLSRouteIndex is the name of the index that maps Gateways to TLSRoutes referencing them in their ParentRefs.
	GatewayOnTLSRouteIndex = "GatewayOnTLSRoute"
)

// OptionsForTLSRoute returns a slice of Option configured for indexing TLSRoute objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForTLSRoute() []Option {
	return []Option{
		{
			Object:         &gwtypes.TLSRoute{},
			Field:          BackendServicesOnTLSRouteIndex,
			ExtractValueFn: BackendServicesOnTLSRoute,
		},
		{
			Object:         &gwtypes.TLSRoute{},
			Field:          GatewayOnTLSRouteIndex,
			ExtractValueFn: GatewaysOnRoute[gwtypes.TLSRoute],
		},
	}
}

// BackendServicesOnTLSRoute extracts and returns a list of unique Service references (in "namespace/name" format)
// from the BackendRefs of the given TLSRoute object.
func BackendServicesOnTLSRoute(o client.Object) []string {
	tlsRoute, ok := o.(*gwtypes.TLSRoute)
	if !ok {
		return nil
	}

	var services []string
	for _, rule := range tlsRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if serviceKey, ok := backendRefToServiceKey(backendRef, tlsRoute.Namespace); ok {
				services = append(services, serviceKey)
			}
		}
	}
	return lo.Uniq(services)
}
