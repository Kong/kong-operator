package diagnostics

import (
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	"github.com/samber/lo"

	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
)

var _ multiinstance.DiagnosticsExposer = &ControlPlaneDiagnosticsExposer{}

// ControlPlaneDiagnosticsExposer exposes ControlPlanes' diagnositics handlers.
type ControlPlaneDiagnosticsExposer struct {
	lock sync.RWMutex

	instances map[manager.ID]http.Handler

	logger logr.Logger
}

// NewControlPlaneDiagnosticsExposer creates an exposer to expose diagnositics of ControlPlanes.
func NewControlPlaneDiagnosticsExposer(logger logr.Logger) *ControlPlaneDiagnosticsExposer {
	return &ControlPlaneDiagnosticsExposer{
		instances: map[manager.ID]http.Handler{},
		logger:    logger,
	}
}

// RegisterInstance registers a new ControlPlane instance.
func (e *ControlPlaneDiagnosticsExposer) RegisterInstance(id manager.ID, handler http.Handler) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.logger.Info("New controller for control plane started", "id", id)
	e.instances[id] = handler
}

// UnregisterInstance unregisters a ControlPlane instance.
func (e *ControlPlaneDiagnosticsExposer) UnregisterInstance(id manager.ID) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.logger.Info("Controller for control plane stopped", "id", id)
	delete(e.instances, id)
}

// listInstances lists all registered ControlPlane instances.
func (e *ControlPlaneDiagnosticsExposer) listInstances() []manager.ID {
	e.lock.RLock()
	defer e.lock.RUnlock()
	return lo.Keys(e.instances)
}

// getHandlerByID gets the registered handler by CP instance ID.
func (e *ControlPlaneDiagnosticsExposer) getHandlerByID(id manager.ID) (http.Handler, bool) {
	e.lock.RLock()
	defer e.lock.RUnlock()
	h, ok := e.instances[id]
	return h, ok
}
