package types

import "sigs.k8s.io/controller-runtime/pkg/client"

// SupportedRoute defines a supported route type in gateway API.
// It also include the client.Object interface to extract metadata.
type SupportedRoute interface {
	*TLSRoute | *HTTPRoute
	client.Object
}

// SupportedRouteRule defines rules in supported gateway API routes.
type SupportedRouteRule interface {
	TLSRouteRule | HTTPRouteRule
}

// SupportedRouteBackendRef defines backend references in supported gateway API routes.
type SupportedRouteBackendRef interface {
	BackendRef | HTTPBackendRef
}
