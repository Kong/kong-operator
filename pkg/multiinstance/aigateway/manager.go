// Package aigateway is able to dynamically run multiple on-prem AI Gateway control plane instances and
// manage their lifecycle. The generic instance lifecycle is shared with the Kong Ingress Controller's
// multi-instance manager through the instances package.
package aigateway

import (
	"context"
	"sync"

	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/instances"
)

// Manager runs multiple on-prem AI Gateway control plane instances.
//
// On top of the shared registry, it keeps the scheduled instances so a caller can
// wait for a stopped instance to actually stop: the registry forgets an instance
// as soon as it is told to stop, while its controllers may still be writing.
type Manager struct {
	*instances.Registry

	mu        sync.Mutex
	scheduled map[manager.ID]*Instance
}

// NewManager creates a new AI Gateway multi-instance manager.
func NewManager(logger logr.Logger, opts ...instances.RegistryOption) *Manager {
	return &Manager{
		Registry:  instances.NewRegistry(logger, opts...),
		scheduled: map[manager.ID]*Instance{},
	}
}

// ScheduleInstance schedules the instance on the registry and keeps track of it.
func (m *Manager) ScheduleInstance(in *Instance) error {
	if err := m.Registry.ScheduleInstance(in); err != nil {
		return err
	}
	m.mu.Lock()
	m.scheduled[in.ID()] = in
	m.mu.Unlock()
	return nil
}

// StopInstance stops the instance with the given ID. It does not wait for the
// instance to stop: use WaitForInstanceStop for that.
func (m *Manager) StopInstance(id manager.ID) error {
	if err := m.Registry.StopInstance(id); err != nil {
		return err
	}
	m.mu.Lock()
	in, ok := m.scheduled[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	in.markStopped()
	go func() {
		<-in.Done()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.scheduled[id] == in {
			delete(m.scheduled, id)
		}
	}()
	return nil
}

// WaitForInstanceStop blocks until the instance with the given ID stopped, or ctx
// is done. It returns at once when no such instance is tracked.
func (m *Manager) WaitForInstanceStop(ctx context.Context, id manager.ID) error {
	m.mu.Lock()
	in, ok := m.scheduled[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case <-in.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
