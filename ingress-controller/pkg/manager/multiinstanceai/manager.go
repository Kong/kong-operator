// Package multiinstanceai is able to dynamically run multiple on-prem AI Gateway control plane instances and
// manage their lifecycle. The generic instance lifecycle is shared with the Kong Ingress Controller's
// multi-instance manager through the instances package.
package multiinstanceai

import (
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

// Manager runs multiple on-prem AI Gateway control plane instances.
//
// It currently adds nothing on top of the shared registry. It exists as the place to put the AI Gateway
// specifics as they land, and to keep the AI Gateway instance type from having to satisfy the Kong Ingress
// Controller's ManagerInstance interface.
type Manager struct {
	*instances.Registry
}

// NewManager creates a new AI Gateway multi-instance manager.
func NewManager(logger logr.Logger, opts ...instances.RegistryOption) *Manager {
	return &Manager{
		Registry: instances.NewRegistry(logger, opts...),
	}
}
