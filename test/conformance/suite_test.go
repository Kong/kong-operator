package conformance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/gateway-api/conformance/utils/flags"
	gwapiv1 "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/typed/apis/v1"

	"github.com/kong/kong-operator/v2/config"
	"github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   bool
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

var (
	ctx    context.Context
	cancel context.CancelFunc
	env    environments.Environment

	clients testutils.K8sClients

	httpc = http.Client{
		Timeout: time.Second * 10,
	}
)

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	var code int
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v stack trace:\n%s\n", r, debug.Stack())
			code = 1
		}
		os.Exit(code)
	}()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	closeControllerLogFile, err := testutils.SetupControllerLogger(controllerManagerOut)
	exitOnErr(err)
	defer closeControllerLogFile() //nolint:errcheck

	fmt.Println("INFO: configuring cluster for testing environment")
	// NOTE: We run the conformance tests on a cluster without a CNI that enforces
	// resources like NetworkPolicy.
	// Running those tests on a cluster with CNI like Calico would break because
	// Gateway API conformance tests do not create resources like NetworkPolicy
	// that would allow e.g. cross namespace traffic.
	// Related upstream discussion: https://github.com/kubernetes-sigs/gateway-api/discussions/2137
	env, err = testutils.BuildEnvironment(ctx, existingCluster, func(b *environments.Builder, t clusters.Type) {
		if !test.IsMetalLBDisabled() {
			b.WithAddons(metallb.New())
		}
	})
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	exitOnErr(<-env.WaitForReady(ctx))

	fmt.Println("INFO: initializing Kubernetes clients")
	clients, err = testutils.NewK8sClients(env)
	exitOnErr(err)

	configPath, cleaner, err := config.DumpKustomizeConfigToTempDir()
	exitOnErr(err)
	defer cleaner()

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), path.Join(configPath, "/rbac/base")))
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), path.Join(configPath, "/rbac/role")))

	// normally this is obtained from the downward API. the tests fake it.
	err = os.Setenv("POD_NAMESPACE", "kong-system")
	exitOnErr(err)

	if !test.IsInstallingCRDsDisabled() {
		fmt.Println("INFO: deploying CRDs to test cluster")
		exitOnErr(testutils.DeployCRDs(ctx, path.Join(configPath, "/crd"), clients.OperatorClient, env.Cluster()))
	}

	cleanupTelepresence, err := helpers.SetupTelepresence(ctx)
	exitOnErr(err)
	defer cleanupTelepresence()

	fmt.Println("INFO: starting the operator's controller manager")
	// startControllerManager will spawn the controller manager in a separate
	// goroutine and will report whether that succeeded.
	metadata := metadata.Metadata()
	started := startControllerManager(metadata)
	<-started

	exitOnErr(testutils.BuildMTLSCredentials(ctx, clients.K8sClient, &httpc))

	fmt.Println("INFO: environment is ready, starting tests")
	code = m.Run()
	if code != 0 {
		output, err := env.Cluster().DumpDiagnostics(ctx, "gateway_api_conformance")
		if err != nil {
			fmt.Printf("ERROR: conformance tests failed and failed to dump the diagnostics: %v\n", err)
		} else {
			fmt.Printf("INFO: conformance tests failed, dumped diagnostics to %s\n", output)
		}
	}

	// If we set the shouldCleanup flag on the conformance suite we need to wait
	// for the operator to handle Gateway finalizers.
	// If we don't do it then we'll be left with Gateways that have a deleted
	// timestamp and finalizers set but no operator running which could handle those.
	if *flags.CleanupBaseResources {
		exitOnErr(waitForConformanceGatewaysToCleanup(ctx, clients.GatewayClient.GatewayV1()))
	}

	if !skipClusterCleanup && existingCluster == "" {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(env.Cleanup(ctx))
	}
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if !skipClusterCleanup && existingCluster == "" {
			if env != nil {
				env.Cleanup(ctx) //nolint:errcheck
			}
		}
		panic(fmt.Sprintf("ERROR: %s\n", err.Error()))
	}
}

// startControllerManager will configure the manager and start it in a separate goroutine.
// It returns a channel which will get closed when manager.Start() gets called.
func startControllerManager(metadata metadata.Info) <-chan struct{} {
	cfg := testutils.DefaultControllerConfigForTests()

	// Disable label selectors for secrets and configMaps to make the secrets and configMaps in the tests reconciled.
	// The secrets (used as listener certificates) does not support adding labels to them,
	// so we need to disable the label selector to get them reconciled:
	// https://github.com/kubernetes-sigs/gateway-api/issues/4056
	cfg.SecretLabelSelector = ""
	cfg.ConfigMapLabelSelector = ""

	startedChan := make(chan struct{})
	go func() {
		exitOnErr(manager.Run(cfg, scheme.Get(), manager.SetupControllers, startedChan, metadata))
	}()

	return startedChan
}

func waitForConformanceGatewaysToCleanup(ctx context.Context, gw gwapiv1.GatewayV1Interface) error {
	const conformanceInfraNamespace = "gateway-conformance-infra"

	var (
		gwClient         = gw.Gateways(conformanceInfraNamespace)
		ticker           = time.NewTicker(100 * time.Millisecond)
		gatewayRemaining = 0
	)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("conformance cleanup failed (%d gateways remain): %w", gatewayRemaining, ctx.Err())
		case <-ticker.C:
			gws, err := gwClient.List(ctx, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to list Gateways in %s namespace during cleanup: %w", conformanceInfraNamespace, err)
			}
			if len(gws.Items) == 0 {
				return nil
			}
			gatewayRemaining = len(gws.Items)
		}
	}
}
