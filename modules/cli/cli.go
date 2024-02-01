package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/logging"
	"github.com/kong/gateway-operator/modules/manager/metadata"
)

// New returns a new CLI.
func New() *CLI {
	flagSet := flag.NewFlagSet("", flag.ExitOnError)

	var cfg manager.Config
	var deferCfg flagsForFurtherEvaluation

	flagSet.BoolVar(&cfg.AnonymousReports, "anonymous-reports", true, "Send anonymized usage data to help improve Kong")
	flagSet.StringVar(&cfg.APIServerPath, "apiserver-host", "", "The Kubernetes API server URL. If not set, the operator will use cluster config discovery.")
	flagSet.StringVar(&cfg.KubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file.")

	flagSet.StringVar(&cfg.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.StringVar(&cfg.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&deferCfg.DisableLeaderElection, "no-leader-election", false,
		"Disable leader election for controller manager. Disabling this will not ensure there is only one active controller manager.")

	flagSet.StringVar(&cfg.ControllerName, "controller-name", "", "a controller name to use if other than the default, only needed for multi-tenancy")
	flagSet.StringVar(&cfg.ClusterCASecretName, "cluster-ca-secret", "kong-operator-ca", "name of the Secret containing the cluster CA certificate")
	flagSet.StringVar(&deferCfg.ClusterCASecretNamespace, "cluster-ca-secret-namespace", "", "name of the namespace for Secret containing the cluster CA certificate")

	// controllers for standard APIs and features
	flagSet.BoolVar(&cfg.GatewayControllerEnabled, "enable-controller-gateway", false, "Enable the Gateway controller.")
	flagSet.BoolVar(&cfg.ControlPlaneControllerEnabled, "enable-controller-controlplane", false, "Enable the ControlPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneControllerEnabled, "enable-controller-dataplane", true, "Enable the DataPlane controller.")
	flagSet.BoolVar(&cfg.DataPlaneBlueGreenControllerEnabled, "enable-controller-dataplane-bluegreen", true, "Enable the DataPlane BlueGreen controller. Mutually exclusive with DataPlane controller.")

	// controllers for specialized APIs and features
	//
	// TODO: code for this controller exists, but it's currently only scaffolding.
	// We will add the reconciliation logic and then enable the flag as part of
	// https://github.com/Kong/gateway-operator/issues/1368
	//
	// flagSet.BoolVar(&cfg.AIGatewayControllerEnabled, "enable-controller-aigateway", false, "Enable the AIGateway controller.")

	// webhook and validation options
	flagSet.BoolVar(&deferCfg.ValidatingWebhookEnabled, "enable-validating-webhook", true, "Enable the validating webhook.")

	flagSet.BoolVar(&deferCfg.Version, "version", false, "Print version information")

	return &CLI{
		flagSet:         flagSet,
		cfg:             &cfg,
		deferFlagValues: &deferCfg,
	}
}

// CLI represents command line interface for the operator.
type CLI struct {
	flagSet *flag.FlagSet

	// deferFlagValues contains values of flags that require additional
	// logic after parsing flagSet to determine desired configuration.
	deferFlagValues *flagsForFurtherEvaluation
	cfg             *manager.Config
}

type flagsForFurtherEvaluation struct {
	DisableLeaderElection    bool
	ClusterCASecretNamespace string
	ValidatingWebhookEnabled bool
	Version                  bool
}

// Parse parses flag definitions from the argument list, which should not include the command name.
// Must be called after all additional flags in the FlagSet() are defined and before flags are accessed
// by the program. It returns config for controller manager.
func (c *CLI) Parse(arguments []string) manager.Config {
	developmentModeEnabled := manager.DefaultConfig().DevelopmentMode
	if v := os.Getenv("CONTROLLER_DEVELOPMENT_MODE"); v == "true" { // TODO: clean env handling https://github.com/Kong/gateway-operator/issues/19
		developmentModeEnabled = true
	}

	webhookCertDir := manager.DefaultConfig().WebhookCertDir
	if certDir := os.Getenv("WEBHOOK_CERT_DIR"); certDir != "" { // TODO: clean env handling https://github.com/Kong/gateway-operator/issues/19
		webhookCertDir = certDir
	}

	loggerOpts := manager.DefaultConfig().LoggerOpts
	loggerOpts.Development = developmentModeEnabled
	loggerOpts.BindFlags(c.flagSet)

	if err := c.flagSet.Parse(arguments); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	validatingWebhookEnabled := c.deferFlagValues.ValidatingWebhookEnabled
	if developmentModeEnabled {
		// If developmentModeEnabled is true, we are running the webhook locally,
		// therefore enabling the validatingWebhook is ineffective and might also be problematic to handle.
		validatingWebhookEnabled = false
	}

	if c.deferFlagValues.Version {
		type Version struct {
			Release string `json:"release"`
			Repo    string `json:"repo"`
			Commit  string `json:"commit"`
		}
		out, err := json.Marshal(Version{
			Release: metadata.Release,
			Repo:    metadata.Repo,
			Commit:  metadata.Commit,
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

	c.cfg.DevelopmentMode = developmentModeEnabled
	c.cfg.LeaderElection = leaderElection
	c.cfg.ControllerNamespace = controllerNamespace
	c.cfg.ClusterCASecretNamespace = clusterCASecretNamespace
	c.cfg.WebhookCertDir = webhookCertDir
	c.cfg.ValidatingWebhookEnabled = validatingWebhookEnabled
	c.cfg.LoggerOpts = logging.SetupLogEncoder(c.cfg.DevelopmentMode, loggerOpts)
	c.cfg.WebhookPort = manager.DefaultConfig().WebhookPort
	c.cfg.LeaderElectionNamespace = controllerNamespace

	return *c.cfg
}

// FlagSet returns bare underlying flagset of the cli. It can be used to register
// additional flags. They will be parsed by Parse() method. Caller needs to take
// care of values set by flags added to this flagset.
func (c *CLI) FlagSet() *flag.FlagSet {
	return c.flagSet
}
