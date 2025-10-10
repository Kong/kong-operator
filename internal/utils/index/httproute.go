package index

import (
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

const (
	// BackendServicesOnHTTPRouteIndex is the name of the index that maps Services to HTTPRoutes
	// referencing them in their backendRefs.
	BackendServicesOnHTTPRouteIndex = "BackendServicesOnHTTPRoute"

	// GatewayOnHTTPRouteIndex is the name of the index that maps Gateways to HTTPRoutes referencing them in their ParentRefs.
	GatewayOnHTTPRouteIndex = "GatewayOnHTTPRoute"
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
			// TODO(mlavacca): support cross-namespace references
			if ns != httpRoute.Namespace {
				continue
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
