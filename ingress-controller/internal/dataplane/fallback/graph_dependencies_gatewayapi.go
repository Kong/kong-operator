package fallback

import (
	"fmt"
	"slices"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// resolveHTTPRouteDependencies resolves potential dependencies for a given HTTPRoute object:
// - Service
// - KongPlugin
// - KongClusterPlugin.
func resolveHTTPRouteDependencies(cache store.CacheStores, route *gwtypes.HTTPRoute) []client.Object {
	return slices.Concat(
		resolveGatewayAPIRouteDependenciesBackendRefs(cache, route, getHTTPRouteBackendRefs(route)),
		resolveObjectDependenciesPlugin(cache, route),
	)
}

// resolveTCPRouteDependencies resolves potential dependencies for a given TCPRoute object:
// - Service
// - KongPlugin
// - KongClusterPlugin.
func resolveTCPRouteDependencies(cache store.CacheStores, route *gwtypes.TCPRoute) []client.Object {
	return slices.Concat(
		resolveGatewayAPIRouteDependenciesBackendRefs(cache, route, getTCPRouteBackendRefs(route)),
		resolveObjectDependenciesPlugin(cache, route),
	)
}

// resolveUDPRouteDependencies resolves potential dependencies for a given UDPRoute object:
// - Service
// - KongPlugin
// - KongClusterPlugin.
func resolveUDPRouteDependencies(cache store.CacheStores, route *gwtypes.UDPRoute) []client.Object {
	return slices.Concat(
		resolveGatewayAPIRouteDependenciesBackendRefs(cache, route, getUDPRouteBackendRefs(route)),
		resolveObjectDependenciesPlugin(cache, route),
	)
}

// resolveTLSRouteDependencies resolves potential dependencies for a given TLSRoute object:
// - Service
// - KongPlugin
// - KongClusterPlugin.
func resolveTLSRouteDependencies(cache store.CacheStores, route *gwtypes.TLSRoute) []client.Object {
	return slices.Concat(
		resolveGatewayAPIRouteDependenciesBackendRefs(cache, route, getTLSRouteBackendRefs(route)),
		resolveObjectDependenciesPlugin(cache, route),
	)
}

// resolveGRPCRouteDependencies resolves potential dependencies for a given GRPCRoute object:
// - Service
// - KongPlugin
// - KongClusterPlugin.
func resolveGRPCRouteDependencies(cache store.CacheStores, route *gwtypes.GRPCRoute) []client.Object {
	return slices.Concat(
		resolveGatewayAPIRouteDependenciesBackendRefs(cache, route, getGRPCRouteBackendRefs(route)),
		resolveObjectDependenciesPlugin(cache, route),
	)
}

// gatewayAPIRoute is an interface that represents a GatewayAPI Route object.
type gatewayAPIRoute interface {
	client.Object
	*gwtypes.HTTPRoute | *gwtypes.TCPRoute | *gwtypes.UDPRoute | *gwtypes.TLSRoute | *gwtypes.GRPCRoute
}

// resolveGatewayAPIRouteDependenciesBackendRefs resolves backend references for a given gatewayAPIRoute object.
func resolveGatewayAPIRouteDependenciesBackendRefs[T gatewayAPIRoute](cache store.CacheStores, route T, backendRefs []gwtypes.BackendRef) []client.Object {
	var dependencies []client.Object
	for _, backendRef := range backendRefs {
		if !util.IsBackendRefGroupKindSupported(backendRef.Group, backendRef.Kind) {
			continue
		}
		ns := route.GetNamespace()
		if backendRef.Namespace != nil {
			ns = string(*backendRef.Namespace)
		}
		ingressClass, exists, err := cache.Service.GetByKey(fmt.Sprintf("%s/%s", ns, backendRef.Name))
		if err == nil && exists {
			dependencies = append(dependencies, ingressClass.(client.Object))
		}
	}
	return dependencies
}

func getHTTPRouteBackendRefs(route *gwtypes.HTTPRoute) []gwtypes.BackendRef {
	var backendRefs []gwtypes.BackendRef
	for _, rule := range route.Spec.Rules {
		backendRefs = append(backendRefs, lo.Map(rule.BackendRefs, func(b gwtypes.HTTPBackendRef, _ int) gwtypes.BackendRef {
			return b.BackendRef
		})...)
	}
	return backendRefs
}

func getTCPRouteBackendRefs(route *gwtypes.TCPRoute) []gwtypes.BackendRef {
	var backendRefs []gwtypes.BackendRef
	for _, rule := range route.Spec.Rules {
		backendRefs = append(backendRefs, rule.BackendRefs...)
	}
	return backendRefs
}

func getUDPRouteBackendRefs(route *gwtypes.UDPRoute) []gwtypes.BackendRef {
	var backendRefs []gwtypes.BackendRef
	for _, rule := range route.Spec.Rules {
		backendRefs = append(backendRefs, rule.BackendRefs...)
	}
	return backendRefs
}

func getTLSRouteBackendRefs(route *gwtypes.TLSRoute) []gwtypes.BackendRef {
	var backendRefs []gwtypes.BackendRef
	for _, rule := range route.Spec.Rules {
		backendRefs = append(backendRefs, rule.BackendRefs...)
	}
	return backendRefs
}

func getGRPCRouteBackendRefs(route *gwtypes.GRPCRoute) []gwtypes.BackendRef {
	var backendRefs []gwtypes.BackendRef
	for _, rule := range route.Spec.Rules {
		backendRefs = append(backendRefs, lo.Map(rule.BackendRefs, func(b gwtypes.GRPCBackendRef, _ int) gwtypes.BackendRef {
			return b.BackendRef
		})...)
	}
	return backendRefs
}
