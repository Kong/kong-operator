package manager_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/ingress-controller/internal/controllers/gateway"
	"github.com/kong/kong-operator/ingress-controller/internal/dataplane"
	"github.com/kong/kong-operator/ingress-controller/internal/konnect"
	"github.com/kong/kong-operator/ingress-controller/internal/license"
	"github.com/kong/kong-operator/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/ingress-controller/internal/util/kubernetes/object/status"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
)

func TestNewConfig(t *testing.T) {
	t.Run("verify defaults are set", func(t *testing.T) {
		defaultConfig, err := manager.NewConfig()
		require.NoError(t, err)

		require.Equal(t, managercfg.Config{
			LogLevel:                               "info",
			LogFormat:                              "text",
			KongAdminAPIConfig:                     managercfg.AdminAPIClientConfig{},
			KongAdminInitializationRetries:         60,
			KongAdminInitializationRetryDelay:      time.Second,
			KongAdminToken:                         "",
			KongAdminTokenPath:                     "",
			KongWorkspace:                          "",
			AnonymousReports:                       true,
			EnableReverseSync:                      false,
			UseLastValidConfigForFallback:          false,
			SyncPeriod:                             10 * time.Hour,
			SkipCACertificates:                     false,
			CacheSyncTimeout:                       2 * time.Minute,
			GracefulShutdownTimeout:                nil,
			APIServerHost:                          "",
			APIServerQPS:                           100,
			APIServerBurst:                         300,
			APIServerCAData:                        nil,
			APIServerCertData:                      nil,
			APIServerKeyData:                       nil,
			MetricsAddr:                            "",
			MetricsAccessFilter:                    "off",
			ProbeAddr:                              ":10254",
			KongAdminURLs:                          []string{"http://localhost:8001"},
			KongAdminSvc:                           managercfg.OptionalNamespacedName{},
			GatewayDiscoveryReadinessCheckInterval: managercfg.DefaultDataPlanesReadinessReconciliationInterval,
			GatewayDiscoveryReadinessCheckTimeout:  managercfg.DefaultDataPlanesReadinessCheckTimeout,
			KongAdminSvcPortNames:                  []string{"admin-tls", "kong-admin-tls"},
			InitCacheSyncDuration:                  dataplane.DefaultCacheSyncWaitDuration,
			ProxySyncInterval:                      dataplane.DefaultSyncInterval,
			ProxySyncTimeout:                       dataplane.DefaultTimeout,
			KubeconfigPath:                         "",
			IngressClassName:                       annotations.DefaultIngressClass,
			LeaderElectionNamespace:                "",
			LeaderElectionID:                       "5b374a9e.konghq.com",
			LeaderElectionForce:                    "",
			Concurrency:                            10,
			FilterTags:                             []string{"managed-by-ingress-controller"},
			WatchNamespaces:                        nil,
			GatewayAPIControllerName:               string(gateway.GetControllerName()),
			Impersonate:                            "",
			EmitKubernetesEvents:                   true,
			ClusterDomain:                          managercfg.DefaultClusterDomain,
			PublishServiceUDP:                      managercfg.OptionalNamespacedName{},
			PublishService:                         managercfg.OptionalNamespacedName{},
			PublishStatusAddress:                   []string{},
			PublishStatusAddressUDP:                []string{},
			UpdateStatus:                           true,
			UpdateStatusQueueBufferSize:            status.DefaultBufferSize,
			IngressNetV1Enabled:                    true,
			IngressClassNetV1Enabled:               true,
			IngressClassParametersEnabled:          true,
			KongClusterPluginEnabled:               true,
			KongPluginEnabled:                      true,
			KongConsumerEnabled:                    true,
			ServiceEnabled:                         true,
			KongUpstreamPolicyEnabled:              true,
			KongServiceFacadeEnabled:               true,
			KongVaultEnabled:                       true,
			KongLicenseEnabled:                     true,
			KongCustomEntityEnabled:                true,
			GatewayAPIGatewayController:            true,
			GatewayAPIHTTPRouteController:          true,
			GatewayAPIReferenceGrantController:     true,
			GatewayAPIGRPCRouteController:          true,
			GatewayAPIBackendTLSRouteController:    true,
			GatewayAPITCPRouteController:           true,
			GatewayAPITLSRouteController:           true,
			GatewayAPIUDPRouteController:           true,
			GatewayToReconcile:                     managercfg.OptionalNamespacedName{},
			SecretLabelSelector:                    nil,
			ConfigMapLabelSelector:                 nil,

			EnableProfiling:      false,
			EnableConfigDumps:    false,
			DumpSensitiveConfig:  false,
			DiagnosticServerPort: consts.DiagnosticsPort,
			FeatureGates:         managercfg.GetFeatureGatesDefaults(),
			TermDelay:            0,
			Konnect: managercfg.KonnectConfig{
				Address:                     "https://us.kic.api.konghq.com",
				InitialLicensePollingPeriod: license.DefaultInitialPollingPeriod,
				LicensePollingPeriod:        license.DefaultPollingPeriod,
				LicenseStorageEnabled:       true,
				UploadConfigPeriod:          managercfg.DefaultKonnectConfigUploadPeriod,
				RefreshNodePeriod:           konnect.DefaultRefreshNodePeriod,
			},
			SplunkEndpoint:                   "",
			SplunkEndpointInsecureSkipVerify: false,
			TelemetryPeriod:                  0,
		},
			defaultConfig,
		)
	})

	t.Run("verify it's possible to override defaults", func(t *testing.T) {
		overrideDiagnosticsServerPort := func(config *managercfg.Config) {
			config.DiagnosticServerPort = 1234
		}
		cfg, err := manager.NewConfig(overrideDiagnosticsServerPort)
		require.NoError(t, err)
		require.Equal(t, 1234, cfg.DiagnosticServerPort)
	})

	t.Run("verify it's possible to override default feature gate", func(t *testing.T) {
		overrideFeatureGate := func(config *managercfg.Config) {
			config.FeatureGates[managercfg.GatewayAlphaFeature] = true
		}
		cfg, err := manager.NewConfig(overrideFeatureGate)
		require.NoError(t, err)
		require.True(t, cfg.FeatureGates[managercfg.GatewayAlphaFeature])
	})
}
