package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ingressmgrconfig "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/modules/manager"
	mgrconfig "github.com/kong/kong-operator/modules/manager/config"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/pkg/consts"
)

// New returns a new CLI.
func New(m metadata.Info) *CLI {
	flagSet := flag.NewFlagSet("", flag.ExitOnError)

	cfg := manager.Config{
		// set default values for MetricsAccessFilter
		MetricsAccessFilter: manager.MetricsAccessFilterOff,
		// set default values for ClusterCAKeyType
		ClusterCAKeyType: mgrconfig.ECDSA,
	}
	var deferCfg flagsForFurtherEvaluation

	flagSet.BoolVar(&cfg.ValidateImages, "validate-images", true, "Validate the images set in ControlPlane and DataPlane specifications.")
	flagSet.Var(newValidatedValue(&cfg.LoggingMode, logging.NewMode, withDefault(logging.ProductionMode)), "logging-mode", "Logging mode to use. Possible values: production, development.")

	flagSet.BoolVar(&cfg.AnonymousReports, "anonymous-reports", true, "Send anonymized usage data to help improve Kong.")
	flagSet.StringVar(&cfg.APIServerPath, "apiserver-host", "", "The Kubernetes API server URL. If not set, the operator will use cluster config discovery.")
	flagSet.StringVar(&cfg.KubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file.")

	flagSet.StringVar(&cfg.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.Var(&cfg.MetricsAccessFilter, "metrics-access-filter", "Specifies the filter access function to be used for accessing the metrics endpoint (possible values: off, rbac). Default is off.")
	flagSet.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&deferCfg.DisableLeaderElection, "no-leader-election", false,
		"Disable leader election for controller manager. Disabling this will not ensure there is only one active controller manager.")
	flagSet.BoolVar(&cfg.EnforceConfig, "enforce-config", true,
		"Enforce the configuration on the generated cluster resources. If set to false, the operator will only enforce the configuration when the owner resource spec changes.")

	flagSet.StringVar(&cfg.ControllerName, "controller-name", "", "Controller name to use if other than the default, only needed for multi-tenancy.")
	flagSet.StringVar(&cfg.ClusterCASecretName, "cluster-ca-secret", "kong-operator-ca", "Name of the Secret containing the cluster CA certificate.")
	flagSet.StringVar(&deferCfg.ClusterCASecretNamespace, "cluster-ca-secret-namespace", "", "Name of the namespace for Secret containing the cluster CA certificate.")
	flagSet.Var(&cfg.ClusterCAKeyType, "cluster-ca-key-type", "Type of the key used for the cluster CA certificate (possible values: ecdsa, rsa). Default: ecdsa.")
	flagSet.IntVar(&cfg.ClusterCAKeySize, "cluster-ca-key-size", mgrconfig.DefaultClusterCAKeySize, "Size (in bits) of the key used for the cluster CA certificate. Only used for RSA keys.")
	flagSet.DurationVar(&cfg.CacheSyncTimeout, "cache-sync-timeout", 0, "The time limit set to wait for syncing controllers' caches. Defaults to 0 to fall back to default from controller-runtime.")
	flagSet.StringVar(&cfg.ClusterDomain, "cluster-domain", ingressmgrconfig.DefaultClusterDomain, "The cluster domain. This is used e.g. in generating addresses for upstream services.")
	flagSet.DurationVar(&cfg.CacheSyncPeriod, "cache-sync-period", 0, "Determine the minimum frequency at which watched resources are reconciled. By default or for 0s value, it falls back to controller-runtime's default.")

	// controllers for standard APIs and features
	flagSet.BoolVar(&cfg.GatewayControllerEnabled, "enable-controller-gateway", true, "Enable the Gateway controller.")
	flagSet.BoolVar(&cfg.ControlPlaneControllerEnabled, "enable-controller-controlplane", true, "Enable the ControlPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneControllerEnabled, "enable-controller-dataplane", true, "Enable the DataPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneBlueGreenControllerEnabled, "enable-controller-dataplane-bluegreen", true, "Enable the DataPlane BlueGreen controller. Mutually exclusive with DataPlane controller.")
	flagSet.BoolVar(&cfg.ControlPlaneExtensionsControllerEnabled, "enable-controller-controlplaneextensions", true, "Enable the ControlPlane extensions controller.")

	// controllers for specialized APIs and features
	flagSet.BoolVar(&cfg.AIGatewayControllerEnabled, "enable-controller-aigateway", false, "Enable the AIGateway controller. (Experimental).")
	flagSet.BoolVar(&cfg.KongPluginInstallationControllerEnabled, "enable-controller-kongplugininstallation", false, "Enable the KongPluginInstallation controller.")
	flagSet.BoolVar(&cfg.GatewayAPIExperimentalEnabled, "enable-gateway-api-experimental", false, "Enable the Gateway API experimental features.")

	// controllers for Konnect APIs
	flagSet.BoolVar(&cfg.KonnectControllersEnabled, "enable-controller-konnect", false, "Enable the Konnect controllers.")
	flagSet.DurationVar(&cfg.KonnectSyncPeriod, "konnect-sync-period", consts.DefaultKonnectSyncPeriod, "Sync period for Konnect entities. After a successful reconciliation of Konnect entities the controller will wait this duration before enforcing configuration on Konnect once again.")
	flagSet.UintVar(&cfg.KonnectMaxConcurrentReconciles, "konnect-controller-max-concurrent-reconciles", consts.DefaultKonnectMaxConcurrentReconciles, "Maximum number of concurrent reconciles for Konnect entities.")

	// webhook and validation options
	var validatingWebhookEnabled bool
	flagSet.BoolVar(&validatingWebhookEnabled, "enable-validating-webhook", false, "Enable the validating webhook. DEPRECATED: This flag is no-op and will be removed in a future release.")
	var validatingWebhookConfigBaseImage string
	flagSet.StringVar(&validatingWebhookConfigBaseImage, "webhook-certificate-config-base-image", consts.WebhookCertificateConfigBaseImage, "The base image for the certgen Jobs. DEPRECATED: This flag is no-op and will be removed in a future release.")
	var validatingWebhookConfigShellImage string
	flagSet.StringVar(&validatingWebhookConfigShellImage, "webhook-certificate-config-shell-image", consts.WebhookCertificateConfigShellImage, "The shell image for the certgen Jobs. DEPRECATED: This flag is no-op and will be removed in a future release.")

	flagSet.BoolVar(&deferCfg.Version, "version", false, "Print version information.")

	loggerOpts := lo.ToPtr(*manager.DefaultConfig().LoggerOpts)
	loggerOpts.BindFlags(flagSet)

	return &CLI{
		flagSet:         flagSet,
		cfg:             &cfg,
		loggerOpts:      loggerOpts,
		deferFlagValues: &deferCfg,
		metadata:        m,
	}
}

// CLI represents command line interface for the operator.
type CLI struct {
	flagSet    *flag.FlagSet
	loggerOpts *zap.Options

	// deferFlagValues contains values of flags that require additional
	// logic after parsing flagSet to determine desired configuration.
	deferFlagValues *flagsForFurtherEvaluation
	cfg             *manager.Config

	metadata metadata.Info
}

type flagsForFurtherEvaluation struct {
	DisableLeaderElection    bool
	ClusterCASecretNamespace string
	Version                  bool
}

const (
	envVarFlagPrefix = "GATEWAY_OPERATOR_"
)

// bindEnvVarsToFlags, for each flag defined on `cmd` (local or parent persistent), looks up the corresponding environment
// variable and (if the flag is unset) takes that environment variable value as the flag value.

func (c *CLI) bindEnvVarsToFlags() (err error) {
	var envKey string
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("environment binding failed for variable %s: %v", envKey, r)
		}
	}()

	c.flagSet.VisitAll(func(f *flag.Flag) {
		envKey = fmt.Sprintf("%s%s", envVarFlagPrefix, strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_")))

		if envValue, envSet := os.LookupEnv(envKey); envSet {
			if err := f.Value.Set(envValue); err != nil {
				panic(err)
			}
		}
	})

	return err
}

// Metadata returns the metadata for the controller manager.
func (c *CLI) Metadata() metadata.Info {
	return c.metadata
}

// Parse parses flag definitions from the argument list, which should not include the command name.
// Must be called after all additional flags in the FlagSet() are defined and before flags are accessed
// by the program. It returns config for controller manager.
func (c *CLI) Parse(arguments []string) manager.Config {
	// Flags take precedence over environment variables,
	// so we bind env vars first then parse arguments to override the values from flags.
	if err := c.bindEnvVarsToFlags(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if err := c.flagSet.Parse(arguments); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if c.deferFlagValues.Version {
		type Version struct {
			Release string `json:"release"`
			Repo    string `json:"repo"`
			Commit  string `json:"commit"`
		}
		out, err := json.Marshal(Version{
			Release: c.metadata.Release,
			Repo:    c.metadata.Repo,
			Commit:  c.metadata.Commit,
		})
		if err != nil {
			fmt.Printf("ERROR: failed to print version information: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s\n", out)
		os.Exit(0)
	}

	leaderElection := manager.DefaultConfig().LeaderElection
	if c.deferFlagValues.DisableLeaderElection {
		leaderElection = false
	}

	controllerNamespace := os.Getenv("POD_NAMESPACE")
	if controllerNamespace == "" {
		controllerNamespace = manager.DefaultConfig().ControllerNamespace
	}

	clusterCASecretNamespace := c.deferFlagValues.ClusterCASecretNamespace
	if clusterCASecretNamespace == "" {
		if controllerNamespace == "" {
			fmt.Println("WARN: -cluster-ca-secret-namespace unset and POD_NAMESPACE env is empty. Please provide namespace for cluster CA secret")
			os.Exit(1)
		} else {
			// If the flag has not been provided then fall back to POD_NAMESPACE env which
			// is normally provided in k8s environment.
			clusterCASecretNamespace = controllerNamespace
		}
	}

	c.cfg.LeaderElection = leaderElection
	c.cfg.ControllerNamespace = controllerNamespace
	c.cfg.ClusterCASecretNamespace = clusterCASecretNamespace
	c.cfg.LoggerOpts = logging.SetupLogEncoder(c.cfg.LoggingMode, c.loggerOpts)
	c.cfg.LeaderElectionNamespace = controllerNamespace

	return *c.cfg
}

// FlagSet returns bare underlying flagset of the cli. It can be used to generate
// documentation for CLI or to register additional flags. They will be parsed by
// Parse() method. Caller needs to take care of values set by flags added to this
// flagset.
func (c *CLI) FlagSet() *flag.FlagSet {
	return c.flagSet
}
