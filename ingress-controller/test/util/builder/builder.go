package builder

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	internalgatewayapi "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	internal "github.com/kong/kong-operator/ingress-controller/internal/util/builder"
)

type EndpointPortBuilder = internal.EndpointPortBuilder
type HTTPBackendRefBuilder = internal.HTTPBackendRefBuilder
type HTTPRouteMatchBuilder = internal.HTTPRouteMatchBuilder
type HTTPRouteFilterBuilder = internal.HTTPRouteFilterBuilder
type ListenerBuilder = internal.ListenerBuilder
type ServicePortBuilder = internal.ServicePortBuilder

func NewEndpointPort(port int32) *EndpointPortBuilder {
	return internal.NewEndpointPort(port)
}

func NewAllowedRoutesFromAllNamespaces() *internalgatewayapi.AllowedRoutes {
	return internal.NewAllowedRoutesFromAllNamespaces()
}

func NewAllowedRoutesFromSameNamespaces() *internalgatewayapi.AllowedRoutes {
	return internal.NewAllowedRoutesFromSameNamespaces()
}

func NewAllowedRoutesFromSelectorNamespace(selector *metav1.LabelSelector) *internalgatewayapi.AllowedRoutes {
	return internal.NewAllowedRoutesFromSelectorNamespace(selector)
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

func NewHTTPRouteRequestRedirectFilter() *HTTPRouteFilterBuilder {
	return internal.NewHTTPRouteRequestRedirectFilter()
}

func NewIngress(name string, class string) *internal.IngressBuilder {
	return internal.NewIngress(name, class)
}
