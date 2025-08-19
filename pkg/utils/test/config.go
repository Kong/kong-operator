package test

import (
	"os"

	"github.com/kong/kong-operator/modules/manager"
	mgrconfig "github.com/kong/kong-operator/modules/manager/config"
	"github.com/kong/kong-operator/modules/manager/logging"
)

// ControllerConfigOption is a function type that modifies a manager.Config.
type ControllerConfigOption func(*manager.Config)

// WithBlueGreenController enables or disables the blue-green controller.
// By default it's disabled.
func WithBlueGreenController(enabled bool) ControllerConfigOption {
	return func(cfg *manager.Config) {
		cfg.DataPlaneBlueGreenControllerEnabled = enabled
	}
}

// DefaultControllerConfigForTests returns a default configuration for the controller manager used in tests.
// It can be adjusted by overriding arbitrary fields in the returned config or by providing config options.
func DefaultControllerConfigForTests(opts ...ControllerConfigOption) manager.Config {
	cfg := manager.DefaultConfig()
	cfg.KubeconfigPath = os.Getenv("KUBECONFIG")
	cfg.LeaderElection = false
	cfg.LoggingMode = logging.DevelopmentMode
	cfg.ControllerName = "konghq.com/gateway-operator-integration-tests"
	cfg.GatewayControllerEnabled = true
	cfg.ControlPlaneControllerEnabled = true
	cfg.ControlPlaneExtensionsControllerEnabled = true
	cfg.DataPlaneControllerEnabled = true
	cfg.DataPlaneBlueGreenControllerEnabled = false
	cfg.KongPluginInstallationControllerEnabled = true
	cfg.AIGatewayControllerEnabled = true
	cfg.AnonymousReports = false
	cfg.KonnectControllersEnabled = true
	cfg.ClusterCAKeyType = mgrconfig.ECDSA
	cfg.GatewayAPIExperimentalEnabled = true
	cfg.EnforceConfig = true
	cfg.ServiceAccountToImpersonate = ServiceAccountToImpersonate
	// TODO: https://github.com/Kong/kong-operator/issues/1986
	cfg.ConversionWebhookEnabled = false
	cfg.ValidationWebhookEnabled = false

	// Apply all the provided options
	for _, opt := range opts {
		opt(&cfg)
	}

	return cfg
}
