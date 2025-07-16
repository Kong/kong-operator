package diagnostics

import (
	"net/http"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
)

var _ multiinstance.DiagnosticsExposer = &ControlPlaneDiagnosticsExposer{}

type ControlPlaneDiagnosticsExposer struct {
	cl     client.Client
	Logger logr.Logger
}

func (e *ControlPlaneDiagnosticsExposer) RegisterInstance(id manager.ID, handler http.Handler) {
	e.Logger.Info("New controller for control plane started", "id", id)
}

func (e *ControlPlaneDiagnosticsExposer) UnregisterInstance(id manager.ID) {
	e.Logger.Info("Controller for control plane stopped", "id", id)
}
