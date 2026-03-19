package types

import "sigs.k8s.io/controller-runtime/pkg/client"

type SupportedRoute interface {
	*TLSRoute | *HTTPRoute
	client.Object
}

type SupportedRouteRule interface {
	TLSRouteRule | HTTPRouteRule
}

type SupportedRouteBackendRef interface {
	BackendRef | HTTPBackendRef
}
