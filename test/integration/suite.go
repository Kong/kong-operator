package integration

import (
	"context"
	"fmt"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"

	"github.com/kong/gateway-operator/config"
	"github.com/kong/gateway-operator/modules/manager"
	mgrconfig "github.com/kong/gateway-operator/modules/manager/config"
	"github.com/kong/gateway-operator/modules/manager/logging"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
	bluegreenController  = strings.ToLower(os.Getenv("GATEWAY_OPERATOR_BLUEGREEN_CONTROLLER")) == "true"
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

var testSuite []func(*testing.T)

// GetTestSuite returns all integration tests that should be run.
func GetTestSuite() []func(*testing.T) {
	return testSuite
}

func addTestsToTestSuite(tests ...func(*testing.T)) {
	testSuite = append(testSuite, tests...)
}

var (
	ctx     context.Context
	env     environments.Environment
	clients testutils.K8sClients
)

// GetCtx returns the context used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain.
func GetCtx() context.Context {
	return ctx
}

// GetEnv returns the environment used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain
func GetEnv() environments.Environment {
	return env
}

// GetClients returns the Kubernetes clients used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain
func GetClients() testutils.K8sClients {
	return clients
}

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

// SetUpAndRunManagerFunc is the type of the callback that is passed to TestMain.
// This id called to set up and run the controller manager. Returned error should be
// a result of calling manager.Run.
type SetUpAndRunManagerFunc func(startedChan chan struct{}) error

// TestMain is the entrypoint for the integration test suite. It bootstraps
// the testing environment and runs the test suite on instance of KGO
// constructed by the argument setUpAndRunManager. This callback is called,
// when the whole cluster is ready and the controller manager can be started.
// Thus it can be used e.g. to apply additional resources to the cluster too.
func TestMain(
	m *testing.M,
	setUpAndRunManager SetUpAndRunManagerFunc,
) {
	var code int
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v stack trace:\n%s\n", r, debug.Stack())
			code = 1
		}
		os.Exit(code)
	}()
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	closeControllerLogFile, err := testutils.SetupControllerLogger(controllerManagerOut)
	exitOnErr(err)
	defer closeControllerLogFile() //nolint:errcheck

	fmt.Println("INFO: configuring cluster for testing environment")
	env, err = testutils.BuildEnvironment(GetCtx(), existingCluster,
		func(b *environments.Builder, ct clusters.Type) {
			if !test.IsCalicoCNIDisabled() {
				b.WithCalicoCNI()
			}
			if !test.IsCertManagerDisabled() {
				b.WithAddons(certmanager.New())
			}
			if !test.IsMetalLBDisabled() && ct == kind.KindClusterType {
				b.WithAddons(metallb.New())
			}
		},
	)
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", GetEnv().Cluster().Name())
	exitOnErr(<-GetEnv().WaitForReady(GetCtx()))

	fmt.Println("INFO: initializing Kubernetes clients")
	clients, err = testutils.NewK8sClients(GetEnv())
	exitOnErr(err)

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(GetCtx(), GetEnv().Cluster(), "kong-system"))

	configPath, cleaner, err := config.DumpKustomizeConfigToTempDir()
	exitOnErr(err)
	defer cleaner()

	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/base")))
	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/role")))
	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/default/validating_policies")))

	// normally this is obtained from the downward API. the tests fake it.
	err = os.Setenv("POD_NAMESPACE", "kong-system")
	exitOnErr(err)

	fmt.Println("INFO: deploying CRDs to test cluster")
	exitOnErr(testutils.DeployCRDs(GetCtx(), path.Join(configPath, "/crd"), GetClients().OperatorClient, GetEnv().Cluster()))

	fmt.Println("INFO: starting the operator's controller manager")
	// Spawn the controller manager based on passed config in
	// a separate goroutine and report whether that succeeded.
	startedChan := make(chan struct{})
	go func() {
		exitOnErr(setUpAndRunManager(startedChan))
	}()
	<-startedChan

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	exitOnErr(err)
	exitOnErr(testutils.BuildMTLSCredentials(GetCtx(), GetClients().K8sClient, httpClient))

	fmt.Println("INFO: environment is ready, starting tests")
	code = m.Run()

	if !skipClusterCleanup && existingCluster == "" {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(GetEnv().Cleanup(GetCtx()))
	}
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if !skipClusterCleanup && existingCluster == "" {
			if GetEnv() != nil {
				GetEnv().Cleanup(GetCtx()) //nolint:errcheck
			}
		}
		panic(fmt.Sprintf("ERROR: %s\n", err.Error()))
	}
}

// DefaultControllerConfigForTests returns a default configuration for the controller manager used in tests.
// It can be adjusted by overriding arbitrary fields in the returned config.
func DefaultControllerConfigForTests() manager.Config {
	cfg := manager.DefaultConfig()
	cfg.LeaderElection = false
	cfg.LoggingMode = logging.DevelopmentMode
	cfg.ControllerName = "konghq.com/gateway-operator-integration-tests"
	cfg.GatewayControllerEnabled = true
	cfg.ControlPlaneControllerEnabled = true
	cfg.DataPlaneControllerEnabled = true
	cfg.DataPlaneBlueGreenControllerEnabled = bluegreenController
	cfg.KongPluginInstallationControllerEnabled = true
	cfg.AIGatewayControllerEnabled = true
	cfg.AnonymousReports = false
	cfg.KonnectControllersEnabled = true
	cfg.ClusterCAKeyType = mgrconfig.ECDSA
	cfg.GatewayAPIExperimentalEnabled = true
	cfg.EnforceConfig = true
	cfg.ServiceAccountToImpersonate = testutils.ServiceAccountToImpersonate

	return cfg
}
