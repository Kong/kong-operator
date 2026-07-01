package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// BackendServicesOnTCPRouteIndex is the name of the index that maps Services to TCPRoutes
	// referencing them in their backendRefs.
	BackendServicesOnTCPRouteIndex = "BackendServicesOnTCPRoute"

	// GatewayOnTCPRouteIndex is the name of the index that maps Gateways to TCPRoutes referencing them in their ParentRefs.
	GatewayOnTCPRouteIndex = "GatewayOnTCPRoute"
)

// OptionsForTCPRoute returns a slice of Option configured for indexing TCPRoute objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForTCPRoute() []Option {
	return []Option{
		{
			Object:         &gwtypes.TCPRoute{},
			Field:          BackendServicesOnTCPRouteIndex,
			ExtractValueFn: BackendServicesOnTCPRoute,
		},
		{
			Object:         &gwtypes.TCPRoute{},
			Field:          GatewayOnTCPRouteIndex,
			ExtractValueFn: GatewaysOnRoute[gwtypes.TCPRoute],
		},
	}
}

// BackendServicesOnTCPRoute extracts and returns a list of unique Service references (in "namespace/name" format)
// from the BackendRefs of the given TCPRoute object.
func BackendServicesOnTCPRoute(o client.Object) []string {
	tcpRoute, ok := o.(*gwtypes.TCPRoute)
	if !ok {
		return nil
	}

	var services []string
	for _, rule := range tcpRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if serviceKey, ok := backendRefToServiceKey(backendRef, tcpRoute.Namespace); ok {
				services = append(services, serviceKey)
			}
		}
	}
	return lo.Uniq(services)
}
