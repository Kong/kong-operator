package builder

import (
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// BackendRefBuilder is a builder for gateway api BackendRef.
// Will set default values, as specified in the gateway API, for fields that are not set.
// Primarily used for testing.
type BackendRefBuilder struct {
	backendRef gwtypes.BackendRef
}

func NewBackendRef(name string) *BackendRefBuilder {
	return &BackendRefBuilder{
		backendRef: gwtypes.BackendRef{
			BackendObjectReference: gwtypes.BackendObjectReference{
				Name: gwtypes.ObjectName(name),
				Kind: util.StringToGatewayAPIKindPtr("Service"), // default value
			},
		},
	}
}

func (b *BackendRefBuilder) Build() gwtypes.BackendRef {
	return b.backendRef
}

func (b *BackendRefBuilder) ToSlice() []gwtypes.BackendRef {
	return []gwtypes.BackendRef{b.backendRef}
}

func (b *BackendRefBuilder) WithPort(port int) *BackendRefBuilder {
	val := gwtypes.PortNumber(port)
	b.backendRef.Port = &val
	return b
}

func (b *BackendRefBuilder) WithWeight(weight int) *BackendRefBuilder {
	b.backendRef.Weight = new(int32(weight))
	return b
}

func (b *BackendRefBuilder) WithKind(kind string) *BackendRefBuilder {
	val := gwtypes.Kind(kind)
	b.backendRef.Kind = &val
	return b
}

func (b *BackendRefBuilder) WithGroup(group string) *BackendRefBuilder {
	val := gwtypes.Group(group)
	b.backendRef.Group = &val
	return b
}

func (b *BackendRefBuilder) WithNamespace(namespace string) *BackendRefBuilder {
	val := gwtypes.Namespace(namespace)
	b.backendRef.Namespace = &val
	return b
}
