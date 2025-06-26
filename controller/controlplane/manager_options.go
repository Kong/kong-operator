package controlplane

import (
	"errors"
	"time"

	"github.com/go-logr/logr"
	managercfg "github.com/kong/kubernetes-ingress-controller/v3/pkg/manager/config"
	"github.com/samber/mo"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

// WithRestConfig sets the REST configuration for the manager, but when a kubeConfigPath is provided,
// it defers to KIC logic to figure out the rest config.
func WithRestConfig(restCfg *rest.Config, kubeConfigPath string) managercfg.Opt {
	return func(c *managercfg.Config) {
		if kubeConfigPath != "" {
			c.KubeconfigPath = kubeConfigPath
		} else {
			c.KubeRestConfig = restCfg
		}
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

// WithKongAdminInitializationRetries sets the Kong Admin initialization retries for the manager.
func WithKongAdminInitializationRetries(retries uint) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.KongAdminInitializationRetries = retries
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

// WithFeatureGates sets the feature gates for the manager.
func WithFeatureGates(logger logr.Logger, featureGates []gwtypes.ControlPlaneFeatureGate) managercfg.Opt {
	return func(c *managercfg.Config) {
		fgs := managercfg.FeatureGates{}
		defaults := managercfg.GetFeatureGatesDefaults()
		for _, feature := range featureGates {
			if _, ok := defaults[feature.Name]; !ok {
				log.Error(logger, errors.New("unknown feature gate"), "unknown feature gate",
					"feature", feature.Name, "state", feature.State,
				)
				continue
			}

			// This should never happen as it should be enforced at the CRD level
			// but we handle it gracefully here and log an error.
			if _, ok := fgs[feature.Name]; ok {
				log.Error(logger, errors.New("feature gate already set"), "feature gate already set",
					"feature", feature.Name, "state", feature.State,
				)
				continue
			}
			fgs[feature.Name] = (feature.State == gwtypes.FeatureGateStateEnabled)
		}

		for k, v := range defaults {
			// Ensure that we don't override the defaults with empty values
			if _, ok := fgs[k]; !ok {
				fgs[k] = v
			}
		}
		c.FeatureGates = fgs
	}
}

// WithControllers sets the controllers for the manager.
func WithControllers(logger logr.Logger, controllers []gwtypes.ControlPlaneController) managercfg.Opt {
	logDeprecated := func(logger logr.Logger, enabled bool, controllerName string) {
		if enabled {
			log.Info(logger, "chosen controller is deprecated", "controller", controllerName)
		}
	}
	setOpt := func(b *bool, state gwtypes.ControllerState) {
		if b == nil {
			return
		}
		*b = (state == gwtypes.ControlPlaneControllerStateEnabled)
	}
	return func(c *managercfg.Config) {
		for _, controller := range controllers {
			switch controller.Name {
			// Ingress related controllers.

			case "INGRESS_NETWORKINGV1":
				setOpt(&c.IngressNetV1Enabled, controller.State)
			case "INGRESS_CLASS_NETWORKINGV1":
				setOpt(&c.IngressClassNetV1Enabled, controller.State)
			case "INGRESS_CLASS_PARAMETERS":
				setOpt(&c.IngressClassParametersEnabled, controller.State)

			// Kong related controllers.

			case "KONG_UDPINGRESS":
				setOpt(&c.UDPIngressEnabled, controller.State)
				logDeprecated(logger, c.UDPIngressEnabled, controller.Name)
			case "KONG_TCPINGRESS":
				setOpt(&c.TCPIngressEnabled, controller.State)
				logDeprecated(logger, c.TCPIngressEnabled, controller.Name)
			case "KONG_INGRESS":
				setOpt(&c.KongIngressEnabled, controller.State)
				logDeprecated(logger, c.KongIngressEnabled, controller.Name)
			case "KONG_CLUSTERPLUGIN":
				setOpt(&c.KongClusterPluginEnabled, controller.State)
			case "KONG_PLUGIN":
				setOpt(&c.KongPluginEnabled, controller.State)
			case "KONG_CONSUMER":
				setOpt(&c.KongConsumerEnabled, controller.State)
			case "KONG_UPSTREAM_POLICY":
				setOpt(&c.KongUpstreamPolicyEnabled, controller.State)
			case "KONG_SERVICE_FACADE":
				setOpt(&c.KongServiceFacadeEnabled, controller.State)
			case "KONG_VAULT":
				setOpt(&c.KongVaultEnabled, controller.State)
			case "KONG_LICENSE":
				setOpt(&c.KongLicenseEnabled, controller.State)
			case "KONG_CUSTOM_ENTITY":
				setOpt(&c.KongCustomEntityEnabled, controller.State)
			case "SERVICE":
				setOpt(&c.ServiceEnabled, controller.State)

			// Gateway API related controllers.

			case "GWAPI_GATEWAY":
				setOpt(&c.GatewayAPIGatewayController, controller.State)
			case "GWAPI_HTTPROUTE":
				setOpt(&c.GatewayAPIHTTPRouteController, controller.State)
			case "GWAPI_GRPCROUTE":
				setOpt(&c.GatewayAPIGRPCRouteController, controller.State)
			case "GWAPI_REFERENCE_GRANT":
				setOpt(&c.GatewayAPIReferenceGrantController, controller.State)

			default:
				// If the controller is not recognized, we can log it or handle it as needed.
				log.Info(logger, "unknown controller", "controller", controller.Name, "state", controller.State)
			}
		}
	}
}
