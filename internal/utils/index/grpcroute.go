package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

const (
	// BackendServicesOnGRPCRouteIndex is the name of the index that maps Services to GRPCRoutes
	// referencing them in their backendRefs.
	BackendServicesOnGRPCRouteIndex = "BackendServicesOnGRPCRoute"

	// GatewayOnGRPCRouteIndex is the name of the index that maps Gateways to GRPCRoutes referencing them in their ParentRefs.
	GatewayOnGRPCRouteIndex = "GatewayOnGRPCRoute"

	// KongPluginsOnGRPCRouteIndex is the name of the index that maps KongPlugins to GRPCRoutes referencing them in their filters.
	KongPluginsOnGRPCRouteIndex = "KongPluginsOnGRPCRoute"
)

// OptionsForGRPCRoute returns a slice of Option configured for indexing GRPCRoute objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForGRPCRoute() []Option {
	return []Option{
		{
			Object:         &gwtypes.GRPCRoute{},
			Field:          BackendServicesOnGRPCRouteIndex,
			ExtractValueFn: BackendServicesOnGRPCRoute,
		},
		{
			Object:         &gwtypes.GRPCRoute{},
			Field:          GatewayOnGRPCRouteIndex,
			ExtractValueFn: GatewaysOnRoute[gwtypes.GRPCRoute],
		},
		{
			Object:         &gwtypes.GRPCRoute{},
			Field:          KongPluginsOnGRPCRouteIndex,
			ExtractValueFn: KongPluginsOnGRPCRoute,
		},
	}
}

// BackendServicesOnGRPCRoute extracts and returns a list of unique Service references (in "namespace/name" format)
// from the BackendRefs of the given GRPCRoute object.
func BackendServicesOnGRPCRoute(o client.Object) []string {
	grpcRoute, ok := o.(*gwtypes.GRPCRoute)
	if !ok {
		return nil
	}

	var services []string
	for _, rule := range grpcRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if serviceKey, ok := backendRefToServiceKey(backendRef.BackendRef, grpcRoute.Namespace); ok {
				services = append(services, serviceKey)
			}
		}
	}
	return lo.Uniq(services)
}

// KongPluginsOnGRPCRoute extracts and returns a list of unique KongPlugin references (in "namespace/name" format)
// from the Filters of the given GRPCRoute object.
func KongPluginsOnGRPCRoute(o client.Object) []string {
	grpcRoute, ok := o.(*gwtypes.GRPCRoute)
	if !ok {
		return nil
	}

	var plugins []string
	for _, rule := range grpcRoute.Spec.Rules {
		for _, filter := range rule.Filters {
			if filter.Type != gatewayv1.GRPCRouteFilterExtensionRef || filter.ExtensionRef == nil {
				continue
			}
			if filter.ExtensionRef.Group != gatewayv1.Group(configurationv1.GroupVersion.Group) || filter.ExtensionRef.Kind != "KongPlugin" {
				continue
			}
			plugins = append(plugins, grpcRoute.Namespace+"/"+string(filter.ExtensionRef.Name))
		}
	}
	return lo.Uniq(plugins)
}
