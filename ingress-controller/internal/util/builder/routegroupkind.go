package builder

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// RouteGroupKindBuilder is a builder for gateway api RouteGroupKind.
// Will set default values, as specified in the gateway API, for fields that are not set.
// Primarily used for testing.
type RouteGroupKindBuilder struct {
	routeGroupKind gwtypes.RouteGroupKind
}

func NewRouteGroupKind() *RouteGroupKindBuilder {
	return &RouteGroupKindBuilder{
		routeGroupKind: gwtypes.RouteGroupKind{
			Group: new(gwtypes.Group(gatewayv1.GroupVersion.Group)),
		},
	}
}

// Build returns the configured RouteGroupKind.
func (b *RouteGroupKindBuilder) Build() gwtypes.RouteGroupKind {
	return b.routeGroupKind
}

// IntoSlice returns the configured RouteGroupKind in a slice.
func (b *RouteGroupKindBuilder) IntoSlice() []gwtypes.RouteGroupKind {
	return []gwtypes.RouteGroupKind{b.routeGroupKind}
}

func (b *RouteGroupKindBuilder) TCPRoute() *RouteGroupKindBuilder {
	b.routeGroupKind.Kind = "TCPRoute"
	return b
}

func (b *RouteGroupKindBuilder) HTTPRoute() *RouteGroupKindBuilder {
	b.routeGroupKind.Kind = "HTTPRoute"
	return b
}

func (b *RouteGroupKindBuilder) UDPRoute() *RouteGroupKindBuilder {
	b.routeGroupKind.Kind = "UDPRoute"
	return b
}

func (b *RouteGroupKindBuilder) TLSRoute() *RouteGroupKindBuilder {
	b.routeGroupKind.Kind = "TLSRoute"
	return b
}

func (b *RouteGroupKindBuilder) GRPCRoute() *RouteGroupKindBuilder {
	b.routeGroupKind.Kind = "GRPCRoute"
	return b
}
