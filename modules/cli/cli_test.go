package cli

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/logging"
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
				"POD_NAMESPACE":               "test",
				"CONTROLLER_DEVELOPMENT_MODE": "true",
			},
			expectedCfg: func() manager.Config {
				cfg := expectedDefaultCfg()
				cfg.LeaderElectionNamespace = "test"
				cfg.ClusterCASecretNamespace = "test"
				cfg.ControllerNamespace = "test"
				// All the below config changes are the result of CONTROLLER_DEVELOPMENT_MODE=true.
				cfg.DevelopmentMode = true
				cfg.ValidatingWebhookEnabled = false
				loggerOpts := manager.DefaultConfig().LoggerOpts
				loggerOpts.Development = true
				cfg.LoggerOpts = logging.SetupLogEncoder(true, loggerOpts)
				return cfg
			},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			for k, v := range tC.envVars {
				t.Setenv(k, v)
			}
			cli := New()

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
		OptionString string
	}

	var additionalCfg additionalConfig
	cli := New()
	cli.FlagSet().BoolVar(&additionalCfg.OptionBool, "additional-bool", true, "Additional bool flag")
	cli.FlagSet().StringVar(&additionalCfg.OptionString, "additional-string", "additional", "Additional string flag")
	// Pass existing flag and a new one to ensure that both work as expected.
	cfg := cli.Parse([]string{"--additional-string=passed", "--metrics-bind-address=:9090"})

	expectedCfg := expectedDefaultCfg()
	expectedCfg.MetricsAddr = ":9090"

	require.Empty(t, cmp.Diff(
		expectedCfg, cfg,
		// Those fields contain functions that are not comparable in Go.
		cmpopts.IgnoreFields(manager.Config{}, "LoggerOpts.EncoderConfigOptions", "LoggerOpts.TimeEncoder")),
	)
	require.Equal(t, additionalConfig{OptionBool: true, OptionString: "passed"}, additionalCfg)
}

func expectedDefaultCfg() manager.Config {
	return manager.Config{
		MetricsAddr:                         ":8080",
		ProbeAddr:                           ":8081",
		WebhookCertDir:                      "/tmp/k8s-webhook-server/serving-certs",
		WebhookPort:                         9443,
		LeaderElection:                      true,
		LeaderElectionNamespace:             "kong-system",
		DevelopmentMode:                     false,
		ControllerName:                      "",
		ControllerNamespace:                 "kong-system",
		AnonymousReports:                    true,
		APIServerPath:                       "",
		KubeconfigPath:                      "",
		ClusterCASecretName:                 "kong-operator-ca",
		ClusterCASecretNamespace:            "kong-system",
		GatewayControllerEnabled:            false,
		ControlPlaneControllerEnabled:       false,
		DataPlaneControllerEnabled:          true,
		DataPlaneBlueGreenControllerEnabled: true,
		ValidatingWebhookEnabled:            true,
	}
}
