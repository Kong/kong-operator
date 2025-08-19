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
	flagSet.StringVar(&cfg.APIServerHost, "apiserver-host", "", "The Kubernetes API server URL. If not set, the operator will use cluster config discovery.")
	flagSet.IntVar(&cfg.APIServerQPS, "apiserver-qps", 100, "The Kubernetes API RateLimiter maximum queries per second.")
	flagSet.IntVar(&cfg.APIServerBurst, "apiserver-burst", 300, "The Kubernetes API RateLimiter maximum burst queries per second.")
	flagSet.StringVar(&cfg.KubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file.")

	flagSet.StringVar(&cfg.SecretLabelSelector, "secret-label-selector", mgrconfig.DefaultSecretLabelSelector, `Limits the secrets ingested to those having this label set to "true". If empty, all secrets are ingested.`)
	flagSet.StringVar(&cfg.ConfigMapLabelSelector, "config-map-label-selector", mgrconfig.DefaultConfigMapLabelSelector, `Limits the configmaps ingested to those having this label set to "true". If empty, all config maps are ingested.`)

	flagSet.StringVar(&cfg.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.Var(&cfg.MetricsAccessFilter, "metrics-access-filter", "Specifies the filter access function to be used for accessing the metrics endpoint (possible values: off, rbac). Default is off.")
	flagSet.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&deferCfg.DisableLeaderElection, "no-leader-election", false,
		"Disable leader election for controller manager. Disabling this will not ensure there is only one active controller manager.")
	flagSet.BoolVar(&cfg.EnforceConfig, "enforce-config", true,
		"Enforce the configuration on the generated cluster resources. If set to false, the operator will only enforce the configuration when the owner resource spec changes.")

	flagSet.StringVar(&cfg.ControllerName, "controller-name", "", "Custom controller name, required only in multi-tenant setups.")
	flagSet.StringVar(&cfg.ClusterCASecretName, "cluster-ca-secret", "kong-operator-ca", "Specifies the Secret name that contains the cluster CA certificate.")
	flagSet.StringVar(&deferCfg.ClusterCASecretNamespace, "cluster-ca-secret-namespace", "", "Specifies the namespace of the Secret that contains the cluster CA certificate.")
	flagSet.Var(&cfg.ClusterCAKeyType, "cluster-ca-key-type", "Type of the key used for the cluster CA certificate (possible values: ecdsa, rsa). Default: ecdsa.")
	flagSet.IntVar(&cfg.ClusterCAKeySize, "cluster-ca-key-size", mgrconfig.DefaultClusterCAKeySize, "Size (in bits) of the key used for the cluster CA certificate. Only used for RSA keys.")
	flagSet.DurationVar(&cfg.CacheSyncTimeout, "cache-sync-timeout", 0, "Sets the time limit for syncing controller caches. Defaults to the controller-runtime value if set to `0`.")
	flagSet.StringVar(&cfg.ClusterDomain, "cluster-domain", ingressmgrconfig.DefaultClusterDomain, "The cluster domain. This is used e.g. in generating addresses for upstream services.")
	flagSet.DurationVar(&cfg.CacheSyncPeriod, "cache-sync-period", 0, "Sets the minimum frequency for reconciling watched resources. Defaults to the controller-runtime value if unspecified or set to 0s.")
	flagSet.BoolVar(&cfg.EmitKubernetesEvents, "emit-kubernetes-events", ingressmgrconfig.DefaultEmitKubernetesEvents, "Emit Kubernetes events for successful configuration applies, translation failures and configuration apply failures on managed objects.")
	flagSet.Var(newValidatedValue(&cfg.WatchNamespaces, manager.NewWatchNamespaces), "watch-namespaces", "Comma-separated list of namespaces to watch. If empty (default), all namespaces are watched.")

	// controllers for standard APIs and features
	flagSet.BoolVar(&cfg.GatewayControllerEnabled, "enable-controller-gateway", true, "Enable the Gateway controller.")
	flagSet.BoolVar(&cfg.ControlPlaneControllerEnabled, "enable-controller-controlplane", true, "Enable the ControlPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneControllerEnabled, "enable-controller-dataplane", true, "Enable the DataPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneBlueGreenControllerEnabled, "enable-controller-dataplane-bluegreen", true, "Enable the DataPlane BlueGreen controller. Mutually exclusive with DataPlane controller.")
	flagSet.BoolVar(&cfg.ControlPlaneExtensionsControllerEnabled, "enable-controller-controlplaneextensions", true, "Enable the ControlPlane extensions controller.")

	// controllers for ControlPlane
	flagSet.BoolVar(&cfg.ControlPlaneConfigurationDumpEnabled, "enable-controlplane-config-dump", false, "Enable the server to dump generated Kong configuration from ControlPlanes. Only effective when ControlPlane controller is enabled.")
	flagSet.StringVar(&cfg.ControlPlaneConfigurationDumpAddr, "controlplane-config-dump-bind-address", manager.DefaultControlPlaneConfigurationDumpAddr, "The address where server dumps ControlPlane configuration. Only enabled when 'enable-controlplane-config-dump' is true.")

	// controllers for specialized APIs and features
	flagSet.BoolVar(&cfg.AIGatewayControllerEnabled, "enable-controller-aigateway", false, "Enable the AIGateway controller. (Experimental).")
	flagSet.BoolVar(&cfg.KongPluginInstallationControllerEnabled, "enable-controller-kongplugininstallation", false, "Enable the KongPluginInstallation controller.")
	flagSet.BoolVar(&cfg.GatewayAPIExperimentalEnabled, "enable-gateway-api-experimental", false, "Enable the Gateway API experimental features.")

	// controllers for Konnect APIs
	flagSet.BoolVar(&cfg.KonnectControllersEnabled, "enable-controller-konnect", false, "Enable the Konnect controllers.")
	flagSet.DurationVar(&cfg.KonnectSyncPeriod, "konnect-sync-period", consts.DefaultKonnectSyncPeriod, "Sync period for Konnect entities. After a successful reconciliation of Konnect entities the controller will wait this duration before enforcing configuration on Konnect once again.")
	flagSet.UintVar(&cfg.KonnectMaxConcurrentReconciles, "konnect-controller-max-concurrent-reconciles", consts.DefaultKonnectMaxConcurrentReconciles, "Maximum number of concurrent reconciles for Konnect entities.")

	flagSet.BoolVar(&deferCfg.Version, "version", false, "Print version information.")

	// webhook and validation options
	flagSet.BoolVar(&cfg.ConversionWebhookEnabled, "enable-conversion-webhook", true, "Enable the conversion webhook.")
	flagSet.BoolVar(&cfg.ValidationWebhookEnabled, "enable-validation-webhook", true, "Enable the validation webhook.")

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
	envDeprecatedVarFlagPrefix = "GATEWAY_OPERATOR_"
	envVarFlagPrefix           = "KONG_OPERATOR_"
)

// setFlagFromEnvVar looks for the env var name corresponding to `f`: if the env val is found its value is set
// to the flag. In case of error it panics.
func setFlagFromEnvVar(f *flag.Flag) {
	envVarFlagName := strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))

	envKey := envVarFlagPrefix + envVarFlagName
	deprecatedEnvKey := envDeprecatedVarFlagPrefix + envVarFlagName
	if envValue, envSet := os.LookupEnv(deprecatedEnvKey); envSet {
		fmt.Printf("WARN: %q env variable is deprecated, please use %q instead\n", deprecatedEnvKey, envKey)
		if err := f.Value.Set(envValue); err != nil {
			panic(fmt.Errorf("environment binding failed for variable %s: %w", deprecatedEnvKey, err))
		}
	}

	if envValue, envSet := os.LookupEnv(envKey); envSet {
		if err := f.Value.Set(envValue); err != nil {
			panic(fmt.Errorf("environment binding failed for variable %s: %w", envKey, err))
		}
	}
}

// bindEnvVarsToFlags, for each flag defined on `cmd` (local or parent persistent), looks up the corresponding environment
// variable and (if the flag is unset) takes that environment variable value as the flag value.
func (c *CLI) bindEnvVarsToFlags() (err error) {
	// setFlagFromEnvVar panics: that way we can recover and extract the variable name that failed.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	c.flagSet.VisitAll(setFlagFromEnvVar)

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
