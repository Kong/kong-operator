package cli

import (
	"flag"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ingressmgrconfig "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/modules/manager"
	mgrconfig "github.com/kong/kong-operator/modules/manager/config"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/pkg/consts"
)

func TestSetFlagFromEnvVar(t *testing.T) {
	testCases := []struct {
		name        string
		flagName    string
		flagVal     int
		expectedVal int
		envVars     map[string]string
		shouldFail  bool
	}{
		{
			name:     "set val from env var",
			flagName: "test-env-var",
			flagVal:  100,
			envVars: map[string]string{
				"KONG_OPERATOR_TEST_ENV_VAR": "200",
			},
			expectedVal: 200,
		},
		{
			name:     "set val from deprecated env var",
			flagName: "test-depenv-var",
			flagVal:  100,
			envVars: map[string]string{
				"GATEWAY_OPERATOR_TEST_DEPENV_VAR": "200",
			},
			expectedVal: 200,
		},
		{
			name:     "ignore val from deprecated env var",
			flagName: "test-depenv-var-ignore",
			flagVal:  100,
			envVars: map[string]string{
				"GATEWAY_OPERATOR_TEST_DEPENV_VAR_IGNORE": "200",
				"KONG_OPERATOR_TEST_DEPENV_VAR_IGNORE":    "300",
			},
			expectedVal: 300,
		},
		{
			name:     "panic on set with wrong val",
			flagName: "test-panic-on-set",
			envVars: map[string]string{
				"KONG_OPERATOR_TEST_PANIC_ON_SET": "fail",
			},
			shouldFail: true,
		},
		{
			name:     "panic on set with wrong val in deprecated env var",
			flagName: "test-panic-on-set-deprecated-var",
			envVars: map[string]string{
				"GATEWAY_OPERATOR_TEST_PANIC_ON_SET_DEPRECATED_VAR": "panic",
			},
			shouldFail: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			for k, v := range tC.envVars {
				t.Setenv(k, v)
			}
			defer func() {
				if r := recover(); r != nil {
					if !tC.shouldFail {
						require.FailNow(t, fmt.Sprintf("test case failed but it shouldn't: %v", r))
					}
				}
			}()
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			val := fs.Int(tC.flagName, tC.flagVal, "")
			setFlagFromEnvVar(fs.Lookup(tC.flagName))
			if tC.shouldFail {
				require.FailNow(t, "test case should fail but it didn't")
			}
			require.Equal(t, tC.expectedVal, *val)
		})
	}
}

func TestParse(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expectedCfg func() manager.Config
		envVars     map[string]string
	}{
		{
			name:        "no command line arguments, no environment variables",
			args:        []string{},
			expectedCfg: expectedDefaultCfg,
		},
		{
			name: "many command line arguments, one environment variable",
			args: []string{"--no-leader-election"},
			envVars: map[string]string{
				"WEBHOOK_CERT_DIR": "/tmp/foo",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.LeaderElection = false
				return cfg
			},
		},
		{
			name: "no command line arguments, logging mode is set to development, anonymous reports off",
			args: []string{},
			envVars: map[string]string{
				"POD_NAMESPACE":                   "test",
				"KONG_OPERATOR_LOGGING_MODE":      "development",
				"KONG_OPERATOR_ANONYMOUS_REPORTS": "false",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.LeaderElectionNamespace = "test"
				cfg.ClusterCASecretNamespace = "test"
				cfg.ControllerNamespace = "test"
				// All the below config changes are the result of KONG_OPERATOR_LOGGING_MODE=development.
				cfg.LoggingMode = logging.DevelopmentMode
				loggerOpts := manager.DefaultConfig().LoggerOpts
				loggerOpts.Development = true
				cfg.LoggerOpts = logging.SetupLogEncoder(logging.DevelopmentMode, loggerOpts)
				cfg.AnonymousReports = false
				cfg.EmitKubernetesEvents = true
				return cfg
			},
		},
		{
			name: "command line arguments takes precedence over environment variables",
			args: []string{
				"--metrics-bind-address=:18080",
			},
			envVars: map[string]string{
				"KONG_OPERATOR_METRICS_BIND_ADDRESS":      ":28080",
				"KONG_OPERATOR_HEALTH_PROBE_BIND_ADDRESS": ":28081",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.MetricsAddr = ":18080" // values from cli args takes precedence
				cfg.ProbeAddr = ":28081"   // env var is present but no cli args is given, use the value from env var
				return cfg
			},
		},
		{
			name: "metrics access filter argument is set",
			args: []string{
				"--metrics-access-filter=rbac",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.MetricsAccessFilter = "rbac"
				return cfg
			},
		},
		{
			name: "cluster CA key type argument is set",
			args: []string{
				"--cluster-ca-key-type=rsa",
				"--cluster-ca-key-size=2048",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.ClusterCAKeySize = 2048
				cfg.ClusterCAKeyType = mgrconfig.RSA
				return cfg
			},
		},
		{
			name: "cluster domain argument is set",
			args: []string{
				"--cluster-domain=foo.bar",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.ClusterDomain = "foo.bar"
				return cfg
			},
		},
		{
			name: "--emit-kubernetes-events argument is set to false",
			args: []string{
				"--emit-kubernetes-events=false",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.EmitKubernetesEvents = false
				return cfg
			},
		},
		{
			name: "deprecated env vars are honored",
			envVars: map[string]string{
				"GATEWAY_OPERATOR_ANONYMOUS_REPORTS":         "false",
				`GATEWAY_OPERATOR_HEALTH_PROBE_BIND_ADDRESS`: ":8090",
				"GATEWAY_OPERATOR_APISERVER_BURST":           "500",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.AnonymousReports = false
				cfg.ProbeAddr = ":8090"
				cfg.APIServerBurst = 500
				return cfg
			},
		},
		{
			name: "newer env vars take precedence over deprecated ones",
			envVars: map[string]string{
				"KONG_OPERATOR_APISERVER_BURST":    "200",
				"GATEWAY_OPERATOR_APISERVER_BURST": "100",
				"GATEWAY_OPERATOR_APISERVER_QPS":   "80",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.APIServerBurst = 200
				cfg.APIServerQPS = 80
				return cfg
			},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			for k, v := range tC.envVars {
				t.Setenv(k, v)
			}
			cli := New(metadata.Metadata())

			cfg := cli.Parse(tC.args)
			require.Empty(t, cmp.Diff(
				tC.expectedCfg(), cfg,
				// Those fields contain functions that are not comparable in Go.
				cmpopts.IgnoreFields(manager.Config{}, "LoggerOpts.EncoderConfigOptions", "LoggerOpts.TimeEncoder")),
			)
		})
	}
}

func expectedDefaultCfg() manager.Config {
	return manager.Config{
		MetricsAddr:                             ":8080",
		MetricsAccessFilter:                     "off",
		ProbeAddr:                               ":8081",
		LeaderElection:                          true,
		LeaderElectionNamespace:                 "kong-system",
		LoggingMode:                             logging.ProductionMode,
		ValidateImages:                          true,
		EnforceConfig:                           true,
		ControllerName:                          "",
		ControllerNamespace:                     "kong-system",
		AnonymousReports:                        true,
		APIServerHost:                           "",
		APIServerQPS:                            100,
		APIServerBurst:                          300,
		KubeconfigPath:                          "",
		SecretLabelSelector:                     mgrconfig.DefaultSecretLabelSelector,
		ConfigMapLabelSelector:                  mgrconfig.DefaultConfigMapLabelSelector,
		ClusterCASecretName:                     "kong-operator-ca",
		ClusterCASecretNamespace:                "kong-system",
		ClusterCAKeyType:                        mgrconfig.ECDSA,
		ClusterCAKeySize:                        mgrconfig.DefaultClusterCAKeySize,
		GatewayControllerEnabled:                true,
		ControlPlaneControllerEnabled:           true,
		DataPlaneControllerEnabled:              true,
		DataPlaneBlueGreenControllerEnabled:     true,
		ControlPlaneConfigurationDumpEnabled:    false,
		ControlPlaneConfigurationDumpAddr:       ":10256",
		ControlPlaneExtensionsControllerEnabled: true,
		KonnectControllersEnabled:               false,
		KonnectSyncPeriod:                       consts.DefaultKonnectSyncPeriod,
		KongPluginInstallationControllerEnabled: false,
		LoggerOpts:                              &zap.Options{},
		KonnectMaxConcurrentReconciles:          consts.DefaultKonnectMaxConcurrentReconciles,
		ClusterDomain:                           ingressmgrconfig.DefaultClusterDomain,
		EmitKubernetesEvents:                    true,
		ConversionWebhookEnabled:                true,
		ValidationWebhookEnabled:                true,
	}
}
