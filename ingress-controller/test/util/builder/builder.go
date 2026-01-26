package builder

import (
	internalgatewayapi "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	internal "github.com/kong/kong-operator/ingress-controller/internal/util/builder"
)

type EndpointPortBuilder = internal.EndpointPortBuilder
type HTTPBackendRefBuilder = internal.HTTPBackendRefBuilder
type HTTPRouteMatchBuilder = internal.HTTPRouteMatchBuilder
type ListenerBuilder = internal.ListenerBuilder
type ServicePortBuilder = internal.ServicePortBuilder

func NewEndpointPort(port int32) *EndpointPortBuilder {
	return internal.NewEndpointPort(port)
}

func NewAllowedRoutesFromAllNamespaces() *internalgatewayapi.AllowedRoutes {
	return internal.NewAllowedRoutesFromAllNamespaces()
}

func NewListener(name string) *ListenerBuilder {
	return internal.NewListener(name)
}

func NewServicePort() *ServicePortBuilder {
	return internal.NewServicePort()
}

func NewHTTPBackendRef(name string) *HTTPBackendRefBuilder {
	return internal.NewHTTPBackendRef(name)
}

func NewHTTPRouteMatch() *HTTPRouteMatchBuilder {
	return internal.NewHTTPRouteMatch()
}
