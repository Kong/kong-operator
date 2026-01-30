package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// BackendServicesOnHTTPRouteIndex is the name of the index that maps Services to HTTPRoutes
	// referencing them in their backendRefs.
	BackendServicesOnHTTPRouteIndex = "BackendServicesOnHTTPRoute"

	// GatewayOnHTTPRouteIndex is the name of the index that maps Gateways to HTTPRoutes referencing them in their ParentRefs.
	GatewayOnHTTPRouteIndex = "GatewayOnHTTPRoute"

	// KongPluginsOnHTTPRouteIndex is the name of the index that maps KongPlugins to HTTPRoutes referencing them in their filters.
	KongPluginsOnHTTPRouteIndex = "KongPluginsOnHTTPRoute"
)

// OptionsForHTTPRoute returns a slice of Option configured for indexing HTTPRoute objects.
// It sets up the index with the appropriate object type, field, and extraction function.
func OptionsForHTTPRoute() []Option {
	return []Option{
		{
			Object:         &gwtypes.HTTPRoute{},
			Field:          BackendServicesOnHTTPRouteIndex,
			ExtractValueFn: backendServicesOnHTTPRoute,
		},
		{
			Object:         &gwtypes.HTTPRoute{},
			Field:          GatewayOnHTTPRouteIndex,
			ExtractValueFn: GatewaysOnHTTPRoute,
		},
		{
			Object:         &gwtypes.HTTPRoute{},
			Field:          KongPluginsOnHTTPRouteIndex,
			ExtractValueFn: KongPluginsOnHTTPRoute,
		},
	}
}

// backendServicesOnHTTPRoute extracts and returns a list of unique Service references (in "namespace/name" format)
// from the BackendRefs of the given HTTPRoute object.
func backendServicesOnHTTPRoute(o client.Object) []string {
	httpRoute, ok := o.(*gwtypes.HTTPRoute)
	if !ok {
		return nil
	}

	var services []string
	for _, rule := range httpRoute.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			if backendRef.Group != nil && *backendRef.Group != "" && *backendRef.Group != "core" {
				continue
			}
			if backendRef.Kind != nil && *backendRef.Kind != "Service" {
				continue
			}
			if backendRef.Name == "" || backendRef.Port == nil {
				continue
			}
			ns := httpRoute.Namespace
			if backendRef.Namespace != nil {
				ns = string(*backendRef.Namespace)
			}

			services = append(services, ns+"/"+string(backendRef.Name))
		}
	}
	return lo.Uniq(services)
}

// GatewaysOnHTTPRoute extracts and returns a list of unique Gateway references (in "namespace/name" format)
// from the ParentRefs of the given HTTPRoute object.
func GatewaysOnHTTPRoute(o client.Object) []string {
	httpRoute, ok := o.(*gwtypes.HTTPRoute)
	if !ok {
		return nil
	}

	var gateways []string
	for _, parentRef := range httpRoute.Spec.ParentRefs {
		// Only consider ParentRefs that refer to Gateways
		if parentRef.Group != nil && *parentRef.Group != "" && *parentRef.Group != "gateway.networking.k8s.io" {
			continue
		}
		if parentRef.Kind != nil && *parentRef.Kind != "Gateway" {
			continue
		}
		ns := httpRoute.Namespace
		if parentRef.Namespace != nil {
			ns = string(*parentRef.Namespace)
		}
		gateways = append(gateways, ns+"/"+string(parentRef.Name))
	}
	return lo.Uniq(gateways)
}

// KongPluginsOnHTTPRoute extracts and returns a list of unique KongPlugin references (in "namespace/name" format)
// from the Filters of the given HTTPRoute object.
func KongPluginsOnHTTPRoute(o client.Object) []string {
	httpRoute, ok := o.(*gwtypes.HTTPRoute)
	if !ok {
		return nil
	}

	var plugins []string
	for _, rule := range httpRoute.Spec.Rules {
		for _, filter := range rule.Filters {
			if filter.Type != gatewayv1.HTTPRouteFilterExtensionRef || filter.ExtensionRef == nil {
				continue
			}
			if filter.ExtensionRef.Group != gatewayv1.Group(configurationv1.GroupVersion.Group) || filter.ExtensionRef.Kind != "KongPlugin" {
				continue
			}
			ns := httpRoute.Namespace
			plugins = append(plugins, ns+"/"+string(filter.ExtensionRef.Name))
		}
	}
	return lo.Uniq(plugins)
}
