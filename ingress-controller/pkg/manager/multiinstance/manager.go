package multiinstance

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/admission"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

const (
	// SchedulingQueueSize is the size of the scheduling queue for manager.Manager instances. It should be large enough
	// to handle all reasonable cases of manager.Manager instances being scheduled at the same time.
	SchedulingQueueSize = instances.SchedulingQueueSize
)

type (
	// InstanceWithIDAlreadyScheduledError is an error indicating that an instance with the same ID is already scheduled.
	InstanceWithIDAlreadyScheduledError = instances.InstanceWithIDAlreadyScheduledError

	// InstanceNotFoundError is an error indicating that an instance with the given ID was not found in the manager.
	// It can indicate that the instance was never scheduled or was stopped.
	InstanceNotFoundError = instances.InstanceNotFoundError

	// DiagnosticsExposer is an interface that represents an object that can expose diagnostics data of manager.Manager
	// instances.
	DiagnosticsExposer = instances.DiagnosticsExposer
)

// NewInstanceWithIDAlreadyScheduledError creates a new InstanceWithIDAlreadyScheduledError for the given ID.
func NewInstanceWithIDAlreadyScheduledError(id manager.ID) InstanceWithIDAlreadyScheduledError {
	return instances.NewInstanceWithIDAlreadyScheduledError(id)
}

// NewInstanceNotFoundError creates a new InstanceNotFoundError for the given ID.
func NewInstanceNotFoundError(id manager.ID) InstanceNotFoundError {
	return instances.NewInstanceNotFoundError(id)
}

// ManagerInstance is an interface that represents a single instance of a manager.Manager, exposing only the methods
// needed by the multi-instance manager.
type ManagerInstance interface {
	ID() manager.ID
	Run(context.Context) error
	ConfigHash() (string, error)
	IsReady() error
	DiagnosticsHandler() http.Handler
	KongValidator() admission.KongHTTPValidator
}

// Manager is able to dynamically run multiple instances of manager.Manager and manage their lifecycle.
// The generic instance lifecycle lives in the embedded instances.Registry; this type only adds the Kong Ingress
// Controller specifics on top of it, i.e. registering instances' admission validators.
type Manager struct {
	*instances.Registry

	admissionReqHandler *admission.RequestHandler
}

// ManagerOption is a functional option that can be used to configure a new multi-instance manager.
type ManagerOption func(*Manager)

// WithDiagnosticsExposer configures the multi-instance manager to
// register diagnostics handlers of manager.Manager instances.
func WithDiagnosticsExposer(exposer DiagnosticsExposer) ManagerOption {
	return func(m *Manager) {
		instances.WithDiagnosticsExposer(exposer)(m.Registry)
	}
}

// WithValidator configures the multi-instance manager to register
// KongHTTPValidators of manager.Manager instances.
func WithValidator(admissionReqHandler *admission.RequestHandler) ManagerOption {
	return func(m *Manager) {
		m.admissionReqHandler = admissionReqHandler
	}
}

// NewManager creates a new multi-instance manager.
func NewManager(logger logr.Logger, opts ...ManagerOption) *Manager {
	m := &Manager{
		Registry: instances.NewRegistry(logger),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// ScheduleInstance adds a new manager.Manager instance to the multi-instance manager and starts it immediately in a
// separate goroutine. If an instance with the same ID already exists, it returns a InstanceWithIDAlreadyScheduledError error.
func (m *Manager) ScheduleInstance(in ManagerInstance) error {
	if m.admissionReqHandler != nil {
		m.admissionReqHandler.RegisterValidator(in.ID(), in.KongValidator())
	}
	if err := m.Registry.ScheduleInstance(in); err != nil {
		return err
	}
	return nil
}

// StopInstance stops a manager.Manager instance with the given ID. If no instance with the given ID exists, it returns
// a InstanceNotFoundError error.
func (m *Manager) StopInstance(instanceID manager.ID) error {
	if err := m.Registry.StopInstance(instanceID); err != nil {
		return err
	}
	if m.admissionReqHandler != nil {
		m.admissionReqHandler.UnregisterValidator(instanceID)
	}
	return nil
}
