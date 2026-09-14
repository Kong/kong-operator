package instances

import (
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

type (
	// Instance is an alias for instances.Instance from the ingress-controller package.
	Instance = instances.Instance

	// Registry is an alias for instances.Registry from the ingress-controller package.
	Registry = instances.Registry

	// RegistryOption is an alias for instances.RegistryOption from the ingress-controller package.
	RegistryOption = instances.RegistryOption
)

// NewRegistry creates a new instances.Registry from the ingress-controller package, wrapped as a local Registry alias.
func NewRegistry(logger logr.Logger, opts ...RegistryOption) *Registry {
	return instances.NewRegistry(logger, opts...)
}
