package controlplane

import (
	"errors"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	operatorv2beta1 "github.com/kong/kong-operator/apis/v2beta1"
	"github.com/kong/kong-operator/controller/pkg/log"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	telemetryTypes "github.com/kong/kong-operator/ingress-controller/pkg/telemetry/types"
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

// WithReverseSync sets whether configuration is sent to Kong even
// if the configuration checksum has not changed since previous update.
func WithReverseSync(state *gwtypes.ControlPlaneReverseSyncState) managercfg.Opt {
	return func(c *managercfg.Config) {
		if state == nil {
			return
		}

		c.EnableReverseSync = *state == gwtypes.ControlPlaneReverseSyncStateEnabled
	}
}

// WithQPSAndBurst sets the QPS and burst for the API server.
func WithQPSAndBurst(qps float32, burst int) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.APIServerQPS = int(qps)
		c.APIServerBurst = burst
	}
}

const (
	// ControllerNameIngress identifies the controller for managing Kubernetes
	// Ingress resources using the networking/v1 API version.
	ControllerNameIngress = "INGRESS_NETWORKINGV1"
	// ControllerNameIngressClass identifies the controller for managing
	// Kubernetes IngressClass resources using the networking/v1 API version.
	ControllerNameIngressClass = "INGRESS_CLASS_NETWORKINGV1"
	// ControllerNameIngressClassParameters identifies the controller for
	// managing IngressClass parameters.
	ControllerNameIngressClassParameters = "INGRESS_CLASS_PARAMETERS"

	// ControllerNameKongClusterPlugin identifies the controller for managing
	// Kong cluster-scoped plugin resources.
	ControllerNameKongClusterPlugin = "KONG_CLUSTERPLUGIN"
	// ControllerNameKongPlugin identifies the controller for managing Kong
	// plugin resources.
	ControllerNameKongPlugin = "KONG_PLUGIN"
	// ControllerNameKongConsumer identifies the controller for managing Kong
	// consumer resources.
	ControllerNameKongConsumer = "KONG_CONSUMER"
	// ControllerNameKongUpstreamPolicy identifies the controller for managing
	// Kong upstream policy resources.
	ControllerNameKongUpstreamPolicy = "KONG_UPSTREAM_POLICY"
	// ControllerNameKongServiceFacade identifies the controller for managing
	// Kong service facade resources.
	ControllerNameKongServiceFacade = "KONG_SERVICE_FACADE"
	// ControllerNameKongVault identifies the controller for managing Kong vault
	// resources.
	ControllerNameKongVault = "KONG_VAULT"
	// ControllerNameKongLicense identifies the controller for managing Kong
	// license resources.
	ControllerNameKongLicense = "KONG_LICENSE"
	// ControllerNameKongCustomEntity identifies the controller for managing
	// Kong custom entity resources.
	ControllerNameKongCustomEntity = "KONG_CUSTOM_ENTITY"
	// ControllerNameService identifies the controller for managing Kubernetes
	// Service resources.
	ControllerNameService = "SERVICE"

	// ControllerNameGatewayAPIGateway identifies the controller for managing
	// Gateway API Gateway resources.
	ControllerNameGatewayAPIGateway = "GWAPI_GATEWAY"
	// ControllerNameGatewayAPIHTTPRoute identifies the controller for managing
	// Gateway API HTTPRoute resources.
	ControllerNameGatewayAPIHTTPRoute = "GWAPI_HTTPROUTE"
	// ControllerNameGatewayAPIGRPCRoute identifies the controller for managing
	// Gateway API GRPCRoute resources.
	ControllerNameGatewayAPIGRPCRoute = "GWAPI_GRPCROUTE"
	// ControllerNameGatewayAPIReferenceGrant identifies the controller for managing
	// Gateway API ReferenceGrant resources.
	ControllerNameGatewayAPIReferenceGrant = "GWAPI_REFERENCE_GRANT"
)

// WithGatewayAPIControllersDisabled disables all Gateway API controllers.
func WithGatewayAPIControllersDisabled() managercfg.Opt {
	return func(c *managercfg.Config) {
		c.GatewayAPIGatewayController = false
		c.GatewayAPIHTTPRouteController = false
		c.GatewayAPIGRPCRouteController = false
		c.GatewayAPIReferenceGrantController = false
		c.GatewayAPIUDPRouteController = false
		c.GatewayAPITCPRouteController = false
		c.GatewayAPITLSRouteController = false
		c.GatewayAPIBackendTLSRouteController = false
	}
}

// WithControllers sets the controllers for the manager.
func WithControllers(logger logr.Logger, controllers []gwtypes.ControlPlaneController) managercfg.Opt {
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

			case ControllerNameIngress:
				setOpt(&c.IngressNetV1Enabled, controller.State)
			case ControllerNameIngressClass:
				setOpt(&c.IngressClassNetV1Enabled, controller.State)
			case ControllerNameIngressClassParameters:
				setOpt(&c.IngressClassParametersEnabled, controller.State)

			// Kong related controllers.

			case ControllerNameKongClusterPlugin:
				setOpt(&c.KongClusterPluginEnabled, controller.State)
			case ControllerNameKongPlugin:
				setOpt(&c.KongPluginEnabled, controller.State)
			case ControllerNameKongConsumer:
				setOpt(&c.KongConsumerEnabled, controller.State)
			case ControllerNameKongUpstreamPolicy:
				setOpt(&c.KongUpstreamPolicyEnabled, controller.State)
			case ControllerNameKongServiceFacade:
				setOpt(&c.KongServiceFacadeEnabled, controller.State)
			case ControllerNameKongVault:
				setOpt(&c.KongVaultEnabled, controller.State)
			case ControllerNameKongLicense:
				setOpt(&c.KongLicenseEnabled, controller.State)
			case ControllerNameKongCustomEntity:
				setOpt(&c.KongCustomEntityEnabled, controller.State)
			case ControllerNameService:
				setOpt(&c.ServiceEnabled, controller.State)

			// Gateway API related controllers.

			case ControllerNameGatewayAPIGateway:
				setOpt(&c.GatewayAPIGatewayController, controller.State)
			case ControllerNameGatewayAPIHTTPRoute:
				setOpt(&c.GatewayAPIHTTPRouteController, controller.State)
			case ControllerNameGatewayAPIGRPCRoute:
				setOpt(&c.GatewayAPIGRPCRouteController, controller.State)
			case ControllerNameGatewayAPIReferenceGrant:
				setOpt(&c.GatewayAPIReferenceGrantController, controller.State)

			default:
				// If the controller is not recognized, we can log it or handle it as needed.
				log.Info(logger, "unknown controller", "controller", controller.Name, "state", controller.State)
			}
		}
	}
}

func managerConfigToStatusControllers(
	cfg managercfg.Config,
) []gwtypes.ControlPlaneController {
	boolToControllerState := func(enabled bool) gwtypes.ControllerState {
		if enabled {
			return gwtypes.ControlPlaneControllerStateEnabled
		}
		return gwtypes.ControlPlaneControllerStateDisabled
	}
	controllers := make([]gwtypes.ControlPlaneController, 0, 19)

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameIngress,
		State: boolToControllerState(cfg.IngressNetV1Enabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameIngressClass,
		State: boolToControllerState(cfg.IngressClassNetV1Enabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameIngressClassParameters,
		State: boolToControllerState(cfg.IngressClassParametersEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongClusterPlugin,
		State: boolToControllerState(cfg.KongClusterPluginEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongPlugin,
		State: boolToControllerState(cfg.KongPluginEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongConsumer,
		State: boolToControllerState(cfg.KongConsumerEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongUpstreamPolicy,
		State: boolToControllerState(cfg.KongUpstreamPolicyEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongServiceFacade,
		State: boolToControllerState(cfg.KongServiceFacadeEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongVault,
		State: boolToControllerState(cfg.KongVaultEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongLicense,
		State: boolToControllerState(cfg.KongLicenseEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameKongCustomEntity,
		State: boolToControllerState(cfg.KongCustomEntityEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameService,
		State: boolToControllerState(cfg.ServiceEnabled),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameGatewayAPIGateway,
		State: boolToControllerState(cfg.GatewayAPIGatewayController),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameGatewayAPIHTTPRoute,
		State: boolToControllerState(cfg.GatewayAPIHTTPRouteController),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameGatewayAPIGRPCRoute,
		State: boolToControllerState(cfg.GatewayAPIGRPCRouteController),
	})

	controllers = append(controllers, gwtypes.ControlPlaneController{
		Name:  ControllerNameGatewayAPIReferenceGrant,
		State: boolToControllerState(cfg.GatewayAPIReferenceGrantController),
	})

	return controllers
}

func managerConfigToStatusFeatureGates(
	cfg managercfg.Config,
) []gwtypes.ControlPlaneFeatureGate {
	featureGates := make([]gwtypes.ControlPlaneFeatureGate, 0, len(cfg.FeatureGates))

	for name, enabled := range cfg.FeatureGates {
		state := gwtypes.FeatureGateStateDisabled
		if enabled {
			state = gwtypes.FeatureGateStateEnabled
		}
		featureGates = append(featureGates, gwtypes.ControlPlaneFeatureGate{
			Name:  name,
			State: state,
		})
	}

	return featureGates
}

// WithAnonymousReports sets whether anonymous usage reports are enabled for the manager.
func WithAnonymousReports(enabled bool) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.AnonymousReports = enabled
	}
}

// WithAnonymousReportsFixedPayloadCustomizer sets a custom payload customizer for anonymous reports.
func WithAnonymousReportsFixedPayloadCustomizer(customizer telemetryTypes.PayloadCustomizer) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.AnonymousReportsFixedPayloadCustomizer = customizer
	}
}

// WithIngressClass sets the ingress class for the manager.
func WithIngressClass(ingressClass *string) managercfg.Opt {
	return func(c *managercfg.Config) {
		if ingressClass == nil || *ingressClass == "" {
			// If ingressClass is nil or empty, we don't set it.
			return
		}
		c.IngressClassName = *ingressClass
	}
}

// WithGatewayDiscoveryReadinessCheckInterval sets the interval for checking
// the readiness of Gateway Discovery.
func WithGatewayDiscoveryReadinessCheckInterval(interval *metav1.Duration) managercfg.Opt {
	return func(c *managercfg.Config) {
		if interval == nil {
			c.GatewayDiscoveryReadinessCheckInterval = managercfg.DefaultDataPlanesReadinessReconciliationInterval
			return
		}
		c.GatewayDiscoveryReadinessCheckInterval = interval.Duration
	}
}

// WithGatewayDiscoveryReadinessCheckTimeout sets the timeout for checking
// the readiness of Gateway Discovery.
func WithGatewayDiscoveryReadinessCheckTimeout(timeout *metav1.Duration) managercfg.Opt {
	return func(c *managercfg.Config) {
		if timeout == nil {
			c.GatewayDiscoveryReadinessCheckTimeout = managercfg.DefaultDataPlanesReadinessCheckTimeout
			return
		}
		c.GatewayDiscoveryReadinessCheckTimeout = timeout.Duration
	}
}

// WithInitCacheSyncDuration sets the initial delay to wait for Kubernetes object caches
// before syncing configuration with dataplanes.
func WithInitCacheSyncDuration(delay time.Duration) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.InitCacheSyncDuration = delay
	}
}

// WithClusterDomain sets the cluster domain for the manager.
func WithClusterDomain(clusterDomain string) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.ClusterDomain = clusterDomain
	}
}

// WithCacheSyncPeriod sets the cache sync period for the manager.
func WithCacheSyncPeriod(period time.Duration) managercfg.Opt {
	return func(c *managercfg.Config) {
		if period <= 0 {
			return
		}
		c.SyncPeriod = period
	}
}

// WithDataPlaneSyncOptions sets the option to sync Kong configuration with managed dataplanes.
func WithDataPlaneSyncOptions(syncOptions gwtypes.ControlPlaneDataPlaneSync) managercfg.Opt {
	return func(c *managercfg.Config) {
		if syncOptions.Interval != nil {
			c.ProxySyncInterval = syncOptions.Interval.Duration
		}
		if syncOptions.Timeout != nil {
			c.ProxySyncTimeout = syncOptions.Timeout.Duration
		}
	}
}

// WithEmitKubernetesEvents sets whether to emit Kubernetes events for the manager.
func WithEmitKubernetesEvents(emit bool) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.EmitKubernetesEvents = emit
	}
}

// WithSecretLabelSelectorMatchLabel sets the label selector to filter ingested secrets.
// It adds a filter that selects the secrets with label `key` with the value specified in `value`.
func WithSecretLabelSelectorMatchLabel(key, value string) managercfg.Opt {
	return func(c *managercfg.Config) {
		if c.SecretLabelSelector == nil {
			c.SecretLabelSelector = map[string]string{}
		}
		c.SecretLabelSelector[key] = value
	}
}

// WithConfigMapLabelSelectorMatchLabel sets the label selector to filter ingested config maps.
// It adds a filter that selects configmaps with label `key` with the value specified in `value`.
func WithConfigMapLabelSelectorMatchLabel(key, value string) managercfg.Opt {
	return func(c *managercfg.Config) {
		if c.ConfigMapLabelSelector == nil {
			c.ConfigMapLabelSelector = map[string]string{}
		}
		c.ConfigMapLabelSelector[key] = value
	}
}

// WithTranslationOptions sets the translation options for the manager.
func WithTranslationOptions(opts *gwtypes.ControlPlaneTranslationOptions) managercfg.Opt {
	return func(c *managercfg.Config) {
		if opts == nil {
			return
		}

		if opts.CombinedServicesFromDifferentHTTPRoutes != nil {
			c.CombinedServicesFromDifferentHTTPRoutes = (*opts.CombinedServicesFromDifferentHTTPRoutes ==
				gwtypes.ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled)
		}
		if fc := opts.FallbackConfiguration; fc != nil && fc.UseLastValidConfig != nil {
			c.UseLastValidConfigForFallback = (*fc.UseLastValidConfig ==
				gwtypes.ControlPlaneFallbackConfigurationStateEnabled)
		}

		if opts.DrainSupport != nil {
			c.EnableDrainSupport = *opts.DrainSupport == gwtypes.ControlPlaneDrainSupportStateEnabled
		}
	}
}

// WithConfigDumpEnabled enables/disables dumping Kong configuration in ControlPlane.
func WithConfigDumpEnabled(enabled bool) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.EnableConfigDumps = enabled
	}
}

// WithSensitiveConfigDumpEnabled enables/disables including sensitive parts in dumped configuration.
func WithSensitiveConfigDumpEnabled(enabled bool) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.DumpSensitiveConfig = enabled
	}
}

// WithWatchNamespaces enables/disables watching namespaces for the manager.
func WithWatchNamespaces(watchNamespaces []string) managercfg.Opt {
	return func(c *managercfg.Config) {
		c.WatchNamespaces = watchNamespaces
	}
}

// WithKonnectOptions merges Konnect options from ControlPlane spec with the existing Konnect configuration.
// Note: c.Konnect is a value field (type managercfg.KonnectConfig), not a pointer,
// so it cannot be nil. When this option is applied via manager.NewConfig(...),
// the base config (including c.Konnect) has already been populated with defaults
// from CLI flag bindings. When used in isolation (e.g., unit tests that start
// from an empty managercfg.Config{}), the zero-value c.Konnect is still safe to
// modify and acts as the default baseline unless an existingKonnectConfig is provided.
func WithKonnectOptions(konnectOptions *operatorv2beta1.ControlPlaneKonnectOptions, existingKonnectConfig *managercfg.KonnectConfig) managercfg.Opt {
	return func(c *managercfg.Config) {
		// Start with existing config if provided
		if existingKonnectConfig != nil {
			c.Konnect = *existingKonnectConfig
		}

		// Apply ControlPlane Konnect options if specified
		if konnectOptions == nil {
			return
		}

		// Configure consumer synchronization
		if konnectOptions.ConsumersSync != nil {
			c.Konnect.ConsumersSyncDisabled = (*konnectOptions.ConsumersSync == operatorv2beta1.ControlPlaneKonnectConsumersSyncStateDisabled)
		}

		// Configure licensing
		if licensing := konnectOptions.Licensing; licensing != nil {
			if licensing.State != nil {
				c.Konnect.LicenseSynchronizationEnabled = (*licensing.State == operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled)
			}
			if licensing.InitialPollingPeriod != nil {
				c.Konnect.InitialLicensePollingPeriod = licensing.InitialPollingPeriod.Duration
			}
			if licensing.PollingPeriod != nil {
				c.Konnect.LicensePollingPeriod = licensing.PollingPeriod.Duration
			}
			if licensing.StorageState != nil {
				c.Konnect.LicenseStorageEnabled = (*licensing.StorageState == operatorv2beta1.ControlPlaneKonnectLicensingStateEnabled)
			}
		}

		// Configure node refresh period
		if konnectOptions.NodeRefreshPeriod != nil {
			c.Konnect.RefreshNodePeriod = konnectOptions.NodeRefreshPeriod.Duration
		}

		// Configure config upload period
		if konnectOptions.ConfigUploadPeriod != nil {
			c.Konnect.UploadConfigPeriod = konnectOptions.ConfigUploadPeriod.Duration
		}
	}
}
