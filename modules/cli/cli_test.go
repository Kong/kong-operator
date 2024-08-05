package cli

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/logging"
	"github.com/kong/gateway-operator/modules/manager/metadata"
)

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
			args: []string{"--no-leader-election", "--enable-validating-webhook=false"},
			envVars: map[string]string{
				"WEBHOOK_CERT_DIR": "/tmp/foo",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.LeaderElection = false
				cfg.ValidatingWebhookEnabled = false
				cfg.WebhookCertDir = "/tmp/foo"
				return cfg
			},
		},
		{
			name: "no command line arguments, many environment variables",
			args: []string{},
			envVars: map[string]string{
				"POD_NAMESPACE":                     "test",
				"GATEWAY_OPERATOR_DEVELOPMENT_MODE": "true",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.LeaderElectionNamespace = "test"
				cfg.ClusterCASecretNamespace = "test"
				cfg.ControllerNamespace = "test"
				// All the below config changes are the result of GATEWAY_OPERATOR_DEVELOPMENT_MODE=true.
				cfg.DevelopmentMode = true
				cfg.ValidatingWebhookEnabled = false
				loggerOpts := manager.DefaultConfig().LoggerOpts
				loggerOpts.Development = true
				cfg.LoggerOpts = logging.SetupLogEncoder(true, loggerOpts)
				cfg.AnonymousReports = false
				return cfg
			},
		},
		{
			name: "command line arguments takes precedence over environment variables",
			args: []string{
				"--metrics-bind-address=:18080",
			},
			envVars: map[string]string{
				"GATEWAY_OPERATOR_METRICS_BIND_ADDRESS":      ":28080",
				"GATEWAY_OPERATOR_HEALTH_PROBE_BIND_ADDRESS": ":28081",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.MetricsAddr = ":18080" // values from cli args takes precedence
				cfg.ProbeAddr = ":28081"   // env var is present but no cli args is given, use the value from env var
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

func TestParseWithAdditionalFlags(t *testing.T) {
	type additionalConfig struct {
		OptionBool   bool
		OptionalInt  int
		OptionString string
	}

	var additionalCfg additionalConfig
	cli := New(metadata.Metadata())
	cli.FlagSet().BoolVar(&additionalCfg.OptionBool, "additional-bool", true, "Additional bool flag")
	cli.FlagSet().StringVar(&additionalCfg.OptionString, "additional-string", "additional", "Additional string flag")
	cli.FlagSet().IntVar(&additionalCfg.OptionalInt, "additional-int", 0, "Additional integer flag")
	// Pass existing flag and a new one to ensure that both work as expected.
	t.Setenv(
		"GATEWAY_OPERATOR_ADDITIONAL_STRING", "failed",
	)
	t.Setenv(
		"GATEWAY_OPERATOR_ADDITIONAL_INT", "1",
	)
	cfg := cli.Parse([]string{"--additional-string=passed", "--metrics-bind-address=:9090"})

	expectedCfg := expectedDefaultCfg()
	expectedCfg.MetricsAddr = ":9090"

	require.Empty(t, cmp.Diff(
		expectedCfg, cfg,
		// Those fields contain functions that are not comparable in Go.
		cmpopts.IgnoreFields(manager.Config{}, "LoggerOpts.EncoderConfigOptions", "LoggerOpts.TimeEncoder")),
	)
	require.Equal(t, additionalConfig{OptionBool: true, OptionString: "passed", OptionalInt: 1}, additionalCfg)
}

func expectedDefaultCfg() manager.Config {
	return manager.Config{
		MetricsAddr:                             ":8080",
		ProbeAddr:                               ":8081",
		WebhookCertDir:                          "/tmp/k8s-webhook-server/serving-certs",
		WebhookPort:                             9443,
		LeaderElection:                          true,
		LeaderElectionNamespace:                 "kong-system",
		DevelopmentMode:                         false,
		ControllerName:                          "",
		ControllerNamespace:                     "kong-system",
		AnonymousReports:                        true,
		APIServerPath:                           "",
		KubeconfigPath:                          "",
		ClusterCASecretName:                     "kong-operator-ca",
		ClusterCASecretNamespace:                "kong-system",
		GatewayControllerEnabled:                true,
		ControlPlaneControllerEnabled:           true,
		DataPlaneControllerEnabled:              true,
		DataPlaneBlueGreenControllerEnabled:     true,
		KonnectControllersEnabled:               false,
		KongPluginInstallationControllerEnabled: true,
		ValidatingWebhookEnabled:                true,
		LoggerOpts:                              &zap.Options{},
	}
}
