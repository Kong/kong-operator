package config

import (
	"fmt"
	"os"
	"time"

	"github.com/cnf/structhash"
	"github.com/samber/mo"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object/status"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/telemetry/types"
)

// Hash computes a hash of the given config.
func Hash(cfg Config) (string, error) {
	// Use structhash to compute a hash of the config.
	// This is used to detect changes in the config of the manager instances.
	return structhash.Hash(cfg, 1)
}

// OptionalNamespacedName is a type that represents a NamespacedName that can be omitted in config.
type OptionalNamespacedName = mo.Option[k8stypes.NamespacedName]

// Opt is a function that modifies a Config.
type Opt func(*Config)

// NewConfig creates a new Config with default values set.
func NewConfig(opts ...Opt) Config {
	cfg := Config{
		// Logging configurations
		LogLevel:  "info",
		LogFormat: "text",

		// Kong high-level controller manager configurations
		KongAdminAPIConfig: AdminAPIClientConfig{
			TLSSkipVerify: false,
			TLSServerName: "",
			CACertPath:    "",
			CACert:        "",
			Headers:       nil,
			TLSClient: TLSClientConfig{
				CertFile: "",
				KeyFile:  "",
				Cert:     "",
				Key:      "",
			},
		},
		KongAdminInitializationRetries:    60,
		KongAdminInitializationRetryDelay: time.Second,
		KongAdminToken:                    "",
		KongAdminTokenPath:                "",
		KongWorkspace:                     "",
		AnonymousReports:                  true,
		EnableReverseSync:                 false,
		UseLastValidConfigForFallback:     false,
		SyncPeriod:                        10 * time.Hour,
		SkipCACertificates:                false,
		CacheSyncTimeout:                  2 * time.Minute,

		// Kong Admin API configuration
		KongAdminURLs:                          []string{"http://localhost:8001"},
		KongAdminSvc:                           mo.None[k8stypes.NamespacedName](),
		KongAdminSvcPortNames:                  []string{"admin-tls", "kong-admin-tls"},
		GatewayDiscoveryReadinessCheckInterval: DefaultDataPlanesReadinessReconciliationInterval,
		GatewayDiscoveryReadinessCheckTimeout:  DefaultDataPlanesReadinessCheckTimeout,

		// Kong Proxy and Proxy Cache configurations
		APIServerQPS:          100,
		APIServerBurst:        300,
		MetricsAccessFilter:   MetricsAccessFilterOff,
		ProbeAddr:             fmt.Sprintf(":%d", consts.HealthzPort),
		ProxySyncInterval:     3 * time.Second,  // dataplane.DefaultSyncInterval
		InitCacheSyncDuration: 5 * time.Second,  // dataplane.DefaultCacheSyncWaitDuration
		ProxySyncTimeout:      30 * time.Second, // dataplane.DefaultTimeout

		// Kubernetes configurations
		GatewayAPIControllerName: "konghq.com/kic-gateway-controller",
		KubeconfigPath:           "",
		IngressClassName:         annotations.DefaultIngressClass,
		LeaderElectionID:         "5b374a9e.konghq.com",
		LeaderElectionNamespace:  "",
		LeaderElectionForce:      "",
		FilterTags:               []string{"managed-by-ingress-controller"},
		Concurrency:              10,
		WatchNamespaces:          nil,
		EmitKubernetesEvents:     true,
		ClusterDomain:            DefaultClusterDomain,

		// Ingress status
		PublishService:              mo.None[k8stypes.NamespacedName](),
		PublishStatusAddress:        []string{},
		PublishServiceUDP:           mo.None[k8stypes.NamespacedName](),
		PublishStatusAddressUDP:     []string{},
		UpdateStatus:                true,
		UpdateStatusQueueBufferSize: status.DefaultBufferSize,

		// Kubernetes API toggling - all enabled by default
		IngressNetV1Enabled:                 true,
		IngressClassNetV1Enabled:            true,
		IngressClassParametersEnabled:       true,
		KongClusterPluginEnabled:            true,
		KongPluginEnabled:                   true,
		KongConsumerEnabled:                 true,
		ServiceEnabled:                      true,
		KongUpstreamPolicyEnabled:           true,
		GatewayAPIGatewayController:         true,
		GatewayAPIHTTPRouteController:       true,
		GatewayAPIReferenceGrantController:  true,
		GatewayAPIGRPCRouteController:       true,
		GatewayAPIBackendTLSRouteController: true,
		GatewayAPITCPRouteController:        true,
		GatewayAPIUDPRouteController:        true,
		GatewayAPITLSRouteController:        true,
		GatewayToReconcile:                  mo.None[k8stypes.NamespacedName](),
		KongServiceFacadeEnabled:            true,
		KongVaultEnabled:                    true,
		KongLicenseEnabled:                  true,
		KongCustomEntityEnabled:             true,

		// Diagnostics
		EnableProfiling:      false,
		EnableConfigDumps:    false,
		DumpSensitiveConfig:  false,
		DiagnosticServerPort: consts.DiagnosticsPort,

		// Drain support
		EnableDrainSupport: consts.DefaultEnableDrainSupport,

		// Combined services from different HTTPRoutes
		CombinedServicesFromDifferentHTTPRoutes: false,

		// Feature Gates - empty map by default
		FeatureGates: GetFeatureGatesDefaults(),

		// SIGTERM or SIGINT signal delay
		TermDelay: 0,

		// Konnect - all disabled by default
		Konnect: KonnectConfig{
			ConfigSynchronizationEnabled:  false,
			LicenseSynchronizationEnabled: false,
			LicenseStorageEnabled:         true,
			InitialLicensePollingPeriod:   time.Minute,    // license.DefaultInitialPollingPeriod,
			LicensePollingPeriod:          12 * time.Hour, // license.DefaultPollingPeriod
			ControlPlaneID:                "",
			Address:                       "https://us.kic.api.konghq.com",
			TLSClient: TLSClientConfig{
				Cert:     "",
				CertFile: "",
				Key:      "",
				KeyFile:  "",
			},
			UploadConfigPeriod:    DefaultKonnectConfigUploadPeriod,
			RefreshNodePeriod:     60 * time.Second, // konnect.DefaultRefreshNodePeriod
			ConsumersSyncDisabled: false,
		},

		// Telemetry settings - defaults
		SplunkEndpoint:                   "",
		SplunkEndpointInsecureSkipVerify: false,
		TelemetryPeriod:                  time.Hour,
	}

	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// Config is the configuration for the Kong Ingress Controller.
type Config struct {
	// Logging configurations
	LogLevel  string
	LogFormat string

	// Kong high-level controller manager configurations
	KongAdminAPIConfig                AdminAPIClientConfig
	KongAdminInitializationRetries    uint
	KongAdminInitializationRetryDelay time.Duration
	KongAdminToken                    string
	KongAdminTokenPath                string
	KongWorkspace                     string
	AnonymousReports                  bool
	EnableReverseSync                 bool
	UseLastValidConfigForFallback     bool
	SyncPeriod                        time.Duration
	SkipCACertificates                bool
	CacheSyncTimeout                  time.Duration
	GracefulShutdownTimeout           *time.Duration

	// Kong Proxy configurations
	APIServerHost                          string
	APIServerQPS                           int
	APIServerBurst                         int
	APIServerCAData                        []byte
	APIServerCertData                      []byte
	APIServerKeyData                       []byte
	MetricsAddr                            string
	MetricsAccessFilter                    MetricsAccessFilter
	ProbeAddr                              string
	KongAdminURLs                          []string
	KongAdminSvc                           OptionalNamespacedName
	GatewayDiscoveryReadinessCheckInterval time.Duration
	GatewayDiscoveryReadinessCheckTimeout  time.Duration
	KongAdminSvcPortNames                  []string
	ProxySyncInterval                      time.Duration
	InitCacheSyncDuration                  time.Duration
	ProxySyncTimeout                       time.Duration

	// KubeRestConfig takes precedence over any fields related to what it configures,
	// such as APIServerHost, APIServerQPS, etc. It's intended to be used when the controller
	// is run as a part of Kong Operator. It bypass the mechanism of constructing this config.
	KubeRestConfig *rest.Config `json:"-"`

	// Kubernetes configurations
	KubeconfigPath           string
	IngressClassName         string
	LeaderElectionNamespace  string
	LeaderElectionID         string
	LeaderElectionForce      string
	Concurrency              int
	FilterTags               []string
	WatchNamespaces          []string
	GatewayAPIControllerName string
	Impersonate              string
	EmitKubernetesEvents     bool
	ClusterDomain            string

	// Ingress status
	PublishServiceUDP       OptionalNamespacedName
	PublishService          OptionalNamespacedName
	PublishStatusAddress    []string
	PublishStatusAddressUDP []string

	UpdateStatus                bool
	UpdateStatusQueueBufferSize int

	// Kubernetes API toggling
	IngressNetV1Enabled           bool
	IngressClassNetV1Enabled      bool
	IngressClassParametersEnabled bool
	KongClusterPluginEnabled      bool
	KongPluginEnabled             bool
	KongConsumerEnabled           bool
	ServiceEnabled                bool
	KongUpstreamPolicyEnabled     bool
	KongServiceFacadeEnabled      bool
	KongVaultEnabled              bool
	KongLicenseEnabled            bool
	KongCustomEntityEnabled       bool

	// Gateway API toggling.
	GatewayAPIGatewayController         bool
	GatewayAPIHTTPRouteController       bool
	GatewayAPIReferenceGrantController  bool
	GatewayAPIGRPCRouteController       bool
	GatewayAPIBackendTLSRouteController bool
	GatewayAPITCPRouteController        bool
	GatewayAPITLSRouteController        bool
	GatewayAPIUDPRouteController        bool

	// GatewayToReconcile specifies the Gateway to be reconciled.
	GatewayToReconcile OptionalNamespacedName

	// SecretLabelSelector specifies the label which will be used to limit the ingestion of secrets. Only those that have this label set to "true" will be ingested.
	SecretLabelSelector map[string]string

	// ConfigMapLabelSelector specifies the label which will be used to limit the ingestion of configmaps. Only those that have this label set to "true" will be ingested.
	ConfigMapLabelSelector map[string]string

	// Diagnostics and performance
	EnableProfiling      bool
	EnableConfigDumps    bool
	DumpSensitiveConfig  bool
	DiagnosticServerPort int
	// TODO: https://github.com/Kong/kubernetes-ingress-controller/issues/7285
	// instead of this toggle, move the server out of the internal.Manager
	DisableRunningDiagnosticsServer bool

	// EnableDrainSupport controls whether to include terminating endpoints in Kong upstreams
	// with weight=0 for graceful connection draining
	EnableDrainSupport bool

	// CombinedServicesFromDifferentHTTPRoutes controls whether we should combine rules from different HTTPRoutes
	// that are sharing the same combination of backends to one Kong service.
	CombinedServicesFromDifferentHTTPRoutes bool

	// Feature Gates
	FeatureGates FeatureGates

	// TermDelay is the time.Duration which the controller manager will wait
	// after receiving SIGTERM or SIGINT before shutting down. This can be
	// helpful for advanced cases with load-balancers so that the ingress
	// controller can be gracefully removed/drained from their rotation.
	TermDelay time.Duration

	Konnect KonnectConfig

	// AnonymousReportsFixedPayloadCustomizer allows customization of anonymous telemetry reports sent by the controller.
	AnonymousReportsFixedPayloadCustomizer types.PayloadCustomizer `json:"-"`
	// Override default telemetry settings (e.g. for testing). They aren't exposed in the CLI.
	SplunkEndpoint                   string
	SplunkEndpointInsecureSkipVerify bool
	TelemetryPeriod                  time.Duration
}

// Resolve modifies the Config object in place by resolving any values that are not set directly (e.g. reading a file
// for a token).
func (c *Config) Resolve() error {
	if c.KongAdminTokenPath != "" {
		token, err := os.ReadFile(c.KongAdminTokenPath)
		if err != nil {
			return fmt.Errorf("failed to read --kong-admin-token-file from path '%s': %w", c.KongAdminTokenPath, err)
		}
		c.KongAdminToken = string(token)
	}
	return nil
}
