package multiinstanceai

import (
	"context"
	"net/http"

	"github.com/cnf/structhash"
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

// Config is the resolved configuration of a single on-prem AI Gateway control plane instance.
//
// It is intentionally empty for now: fields land alongside the OnPremAIGateway spec fields that feed them
// and the configuration assembly that consumes them.
// TODO: https://github.com/Kong/kong-operator/issues/5569
type Config struct{}

// Hash computes a hash of the given config. It's used to detect configuration drift of running instances.
func Hash(cfg Config) (string, error) {
	return structhash.Hash(cfg, 1)
}

// Instance is a single on-prem AI Gateway control plane instance.
type Instance struct {
	id     manager.ID
	cfg    Config
	logger logr.Logger
}

var _ instances.Instance = &Instance{}

// NewInstance creates a new on-prem AI Gateway control plane instance. It does not start it.
func NewInstance(id manager.ID, logger logr.Logger, cfg Config) *Instance {
	return &Instance{
		id:     id,
		cfg:    cfg,
		logger: logger.WithValues("instanceID", id.String()),
	}
}

// ID returns the unique identifier of the instance.
func (i *Instance) ID() manager.ID {
	return i.id
}

// Config returns the configuration of the instance.
func (i *Instance) Config() Config {
	return i.cfg
}

// ConfigHash returns a hash of the instance's configuration.
func (i *Instance) ConfigHash() (string, error) {
	return Hash(i.cfg)
}

// IsReady returns an error if the instance is not ready yet.
//
// There is nothing to wait for yet: the instance does not talk to anything on startup. Once configuration
// assembly and push land, this has to report the actual readiness of the pushing machinery.
// TODO: https://github.com/Kong/kong-operator/issues/5569
func (i *Instance) IsReady() error {
	return nil
}

// DiagnosticsHandler returns nil: the instance does not expose diagnostics data yet.
// TODO: https://github.com/Kong/kong-operator/issues/5630
func (i *Instance) DiagnosticsHandler() http.Handler {
	return nil
}

// Run runs the instance. It blocks until the passed context is cancelled.
//
// It's a no-op for now: it holds nothing but the instance's identity and configuration. Configuration
// assembly and pushing it to the AI Gateway data planes go here.
// TODO: https://github.com/Kong/kong-operator/issues/5569
func (i *Instance) Run(ctx context.Context) error {
	i.logger.Info("Running on-prem AI Gateway control plane instance")
	<-ctx.Done()
	i.logger.Info("Stopped on-prem AI Gateway control plane instance")
	return nil
}
