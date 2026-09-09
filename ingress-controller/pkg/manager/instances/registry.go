// Package instances provides a generic registry able to run multiple long-running instances (e.g. control plane
// instances) and manage their lifecycle. It is intentionally free of any Kong or Kubernetes specifics so that it
// can be shared by the different kinds of in-process control planes the operator runs.
package instances

import (
	"context"
	"net/http"
	"runtime/pprof"
	"sync"

	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
)

const (
	// SchedulingQueueSize is the size of the scheduling queue for instances. It should be large enough
	// to handle all reasonable cases of instances being scheduled at the same time.
	SchedulingQueueSize = 100
)

// Instance is a single long-running instance that a Registry can run, exposing only the methods needed by the
// Registry.
type Instance interface {
	// ID returns the unique identifier of the instance.
	ID() manager.ID

	// Run runs the instance. It should block until the passed context is cancelled.
	Run(context.Context) error

	// IsReady returns an error if the instance is not ready yet.
	IsReady() error

	// ConfigHash returns a hash of the instance's configuration. It is computed once, when the instance is
	// scheduled, and is used to detect configuration drift.
	ConfigHash() (string, error)

	// DiagnosticsHandler returns an HTTP handler exposing the instance's diagnostics data. It can return nil if
	// the instance does not expose any.
	DiagnosticsHandler() http.Handler
}

// DiagnosticsExposer is an interface that represents an object that can expose diagnostics data of instances.
type DiagnosticsExposer interface {
	// RegisterInstance registers a new instance with the diagnostics exposer.
	RegisterInstance(manager.ID, http.Handler)

	// UnregisterInstance unregisters an instance with the diagnostics exposer.
	UnregisterInstance(manager.ID)
}

// Registry is able to dynamically run multiple Instances and manage their lifecycle.
// It is responsible for things like:
// - Making sure there's only one instance with a given ID.
// - Starting and stopping instances as needed.
// - Registering instances' diagnostic handlers in a DiagnosticsExposer when configured.
type Registry struct {
	logger logr.Logger

	instances          map[manager.ID]*instance
	instancesLock      sync.RWMutex
	schedulingQueue    chan manager.ID
	diagnosticsExposer DiagnosticsExposer
}

// RegistryOption is a functional option that can be used to configure a new Registry.
type RegistryOption func(*Registry)

// WithDiagnosticsExposer configures the registry to register diagnostics handlers of instances.
func WithDiagnosticsExposer(exposer DiagnosticsExposer) RegistryOption {
	return func(r *Registry) {
		r.diagnosticsExposer = exposer
	}
}

// NewRegistry creates a new instances registry.
func NewRegistry(logger logr.Logger, opts ...RegistryOption) *Registry {
	r := &Registry{
		logger:          logger,
		instances:       make(map[manager.ID]*instance),
		schedulingQueue: make(chan manager.ID, SchedulingQueueSize),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Start starts the registry and blocks until the context is canceled. It should only be called once.
func (r *Registry) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case instanceID := <-r.schedulingQueue:
			go r.runInstance(ctx, instanceID)
		}
	}
}

// ScheduleInstance adds a new instance to the registry and starts it immediately in a separate goroutine.
// If an instance with the same ID already exists, it returns an InstanceWithIDAlreadyScheduledError error.
func (r *Registry) ScheduleInstance(in Instance) error {
	r.logger.Info("Scheduling instance", "instanceID", in.ID())

	r.instancesLock.Lock()
	defer r.instancesLock.Unlock()

	if _, exists := r.instances[in.ID()]; exists {
		return NewInstanceWithIDAlreadyScheduledError(in.ID())
	}
	// Keep track of the instance, but do not start it from here.
	instance, err := newInstance(in, r.logger)
	if err != nil {
		return err
	}
	r.instances[in.ID()] = instance

	// Send a signal to the scheduling channel to start the instance.
	r.schedulingQueue <- in.ID()

	return nil
}

// StopInstance stops an instance with the given ID. If no instance with the given ID exists, it returns
// an InstanceNotFoundError error.
func (r *Registry) StopInstance(instanceID manager.ID) error {
	r.logger.Info("Stopping instance", "instanceID", instanceID)

	r.instancesLock.Lock()
	defer r.instancesLock.Unlock()

	in, exists := r.instances[instanceID]
	if !exists {
		return NewInstanceNotFoundError(instanceID)
	}

	// If diagnostics are enabled, unregister the instance from the diagnostics exposer.
	if r.diagnosticsExposer != nil {
		r.diagnosticsExposer.UnregisterInstance(instanceID)
	}

	// Send a signal to the instance to stop and let the running goroutine handle the cleanup.
	in.Stop()

	return nil
}

// IsInstanceReady checks if an instance with the given ID is ready. If no instance with the given ID exists,
// it returns an InstanceNotFoundError error.
func (r *Registry) IsInstanceReady(id manager.ID) error {
	r.instancesLock.RLock()
	defer r.instancesLock.RUnlock()
	in, ok := r.instances[id]
	if !ok {
		return NewInstanceNotFoundError(id)
	}
	return in.IsReady()
}

// GetInstanceConfigHash returns the hash of the configuration of an instance with the given ID.
// If no instance with the given ID exists, it returns an InstanceNotFoundError.
func (r *Registry) GetInstanceConfigHash(id manager.ID) (string, error) {
	r.instancesLock.RLock()
	defer r.instancesLock.RUnlock()
	in, ok := r.instances[id]
	if !ok {
		return "", NewInstanceNotFoundError(id)
	}
	return in.ConfigHash(), nil
}

func (r *Registry) runInstance(ctx context.Context, instanceID manager.ID) {
	r.instancesLock.RLock()
	in, exists := r.instances[instanceID]
	r.instancesLock.RUnlock()

	if !exists {
		// Instance was removed while waiting for the lock.
		r.logger.WithValues("instanceID", instanceID).Info("Instance was removed while waiting for the lock")
		return
	}

	r.logger.Info("Starting instance", "instanceID", instanceID)

	// Wrap with pprof.Do to add instanceID to the pprof labels. That will make it easier to identify which instance
	// is responsible for the CPU consumption.
	pprof.Do(ctx, pprof.Labels("instanceID", instanceID.String()), func(ctx context.Context) {
		go in.Run(ctx)
	})

	// If diagnostics are enabled, register the instance with the diagnostics exposer.
	if r.diagnosticsExposer != nil {
		r.diagnosticsExposer.RegisterInstance(instanceID, in.DiagnosticsHandler())
	}

	// Wait for the instance to stop or the parent context be done.
	select {
	case <-in.StopChannel():
		r.logger.Info("Instance stopped, removing it from managed instances", "instanceID", instanceID)
		r.instancesLock.Lock()
		delete(r.instances, instanceID)
		r.instancesLock.Unlock()
	case <-ctx.Done():
	}
}
