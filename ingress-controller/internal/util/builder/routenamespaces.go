package builder

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// RouteNamespacesBuilder is a builder for gateway api RouteNamespaces.
// Will set default values, as specified in the gateway API, for fields that are not set.
// Primarily used for testing.
type RouteNamespacesBuilder struct {
	routeNamespaces gwtypes.RouteNamespaces
}

func NewRouteNamespaces() *RouteNamespacesBuilder {
	return &RouteNamespacesBuilder{}
}

// Build returns the configured RouteNamespaces.
func (b *RouteNamespacesBuilder) Build() *gwtypes.RouteNamespaces {
	return &b.routeNamespaces
}

func (b *RouteNamespacesBuilder) FromSame() *RouteNamespacesBuilder {
	b.routeNamespaces.From = new(gwtypes.NamespacesFromSame)
	return b
}

func (b *RouteNamespacesBuilder) FromAll() *RouteNamespacesBuilder {
	b.routeNamespaces.From = new(gwtypes.NamespacesFromAll)
	return b
}

func (b *RouteNamespacesBuilder) FromSelector(s *metav1.LabelSelector) *RouteNamespacesBuilder {
	b.routeNamespaces.From = new(gwtypes.NamespacesFromSelector)
	b.routeNamespaces.Selector = s
	return b
}
