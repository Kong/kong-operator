package builder

import (
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// HTTPBackendRefBuilder is a builder for gateway api HTTPBackendRef.
// Will set default values, as specified in the gateway API, for fields that are not set.
// Primarily used for testing.
type HTTPBackendRefBuilder struct {
	httpBackendRef gwtypes.HTTPBackendRef
}

func NewHTTPBackendRef(name string) *HTTPBackendRefBuilder {
	return &HTTPBackendRefBuilder{
		httpBackendRef: gwtypes.HTTPBackendRef{
			BackendRef: gwtypes.BackendRef{
				BackendObjectReference: gwtypes.BackendObjectReference{
					Name: gwtypes.ObjectName(name),
					Kind: util.StringToGatewayAPIKindPtr("Service"), // default value
					Port: new(gwtypes.PortNumber(80)),
				},
			},
		},
	}
}

func (b *HTTPBackendRefBuilder) Build() gwtypes.HTTPBackendRef {
	return b.httpBackendRef
}

func (b *HTTPBackendRefBuilder) ToSlice() []gwtypes.HTTPBackendRef {
	return []gwtypes.HTTPBackendRef{b.httpBackendRef}
}

func (b *HTTPBackendRefBuilder) WithPort(port int) *HTTPBackendRefBuilder {
	val := gwtypes.PortNumber(port)
	b.httpBackendRef.Port = &val
	return b
}

func (b *HTTPBackendRefBuilder) WithWeight(weight int) *HTTPBackendRefBuilder {
	b.httpBackendRef.Weight = new(int32(weight))
	return b
}

func (b *HTTPBackendRefBuilder) WithKind(kind string) *HTTPBackendRefBuilder {
	val := gwtypes.Kind(kind)
	b.httpBackendRef.Kind = &val
	return b
}

func (b *HTTPBackendRefBuilder) WithGroup(group string) *HTTPBackendRefBuilder {
	val := gwtypes.Group(group)
	b.httpBackendRef.Group = &val
	return b
}

func (b *HTTPBackendRefBuilder) WithNamespace(namespace string) *HTTPBackendRefBuilder {
	val := gwtypes.Namespace(namespace)
	b.httpBackendRef.Namespace = &val
	return b
}
