package controlplane

import (
	"time"

	managercfg "github.com/kong/kubernetes-ingress-controller/v3/pkg/manager/config"
	"github.com/samber/mo"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/kong/gateway-operator/pkg/vars"
)

// WithRestConfig sets the REST configuration for the manager.
func WithRestConfig(restCfg *rest.Config) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.APIServerHost = restCfg.Host
		c.APIServerCertData = restCfg.CertData
		c.APIServerKeyData = restCfg.KeyData
		c.APIServerCAData = restCfg.CAData
	}
}

// WithKongAdminService sets the Kong Admin service for the manager.
func WithKongAdminService(s types.NamespacedName) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminSvc = mo.Some(s)
	}
}

// WithKongAdminServicePortName sets the Kong Admin service port name for the manager.
func WithKongAdminServicePortName(portName string) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminSvcPortNames = []string{portName}
	}
}

// WithKongAdminInitializationRetryDelay sets the Kong Admin initialization retry delay for the manager.
func WithKongAdminInitializationRetryDelay(delay time.Duration) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminInitializationRetryDelay = delay
	}
}

// WithKongAdminInitializationRetryAttempts sets the Kong Admin initialization retry attempts for the manager.
func WithKongAdminInitializationRetryAttempts(attempts uint) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminInitializationRetries = attempts
	}
}

// WithGatewayToReconcile sets the gateway to reconcile for the manager.
func WithGatewayToReconcile(gateway types.NamespacedName) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.GatewayToReconcile = mo.Some(gateway)
	}
}

// WithGatewayAPIControllerName sets the Gateway API controller name for the manager.
func WithGatewayAPIControllerName() managercfg.Opt {
	return func(c *managercfg.Config) {
		c.GatewayAPIControllerName = vars.ControllerName()
	}
}

// WithKongAdminAPIConfig sets the Kong Admin API configuration for the manager.
func WithKongAdminAPIConfig(cfg managercfg.AdminAPIClientConfig) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminAPIConfig = cfg
	}
}

// WithDisabledLeaderElection disables leader election for the manager.
func WithDisabledLeaderElection() managercfg.Opt {
	return func(c *managercfg.Config) {
		c.LeaderElectionForce = "disabled"
	}
}

// WithPublishService sets the publish service for the manager.
func WithPublishService(service types.NamespacedName) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.PublishService = mo.Some(service)
	}
}

// WithMetricsServerOff disables the metrics server for the manager.
func WithMetricsServerOff() managercfg.Opt {
	return func(c *managercfg.Config) {
		c.MetricsAddr = "0" // 0 disables metrics server
	}
}
