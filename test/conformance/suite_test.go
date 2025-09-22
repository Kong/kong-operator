package conformance

import (
	"context"
	"fmt"
	"os"
	"path"
	"runtime/debug"
	"testing"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/typed/apis/v1"

	"github.com/kong/kong-operator/config"
	"github.com/kong/kong-operator/modules/manager"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overridable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

var (
	env     environments.Environment
	ctx     context.Context
	clients testutils.K8sClients
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
	var cancel context.CancelFunc
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

	// Normally this is obtained from the downward API. The tests fake it.
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

	httpClient := cleanhttp.DefaultClient()
	httpClient.Timeout = 10 * time.Second
	exitOnErr(testutils.BuildMTLSCredentials(ctx, clients.K8sClient, httpClient))

	fmt.Println("INFO: environment is ready, starting tests")
	if code = m.Run(); code != 0 {
		output, err := env.Cluster().DumpDiagnostics(ctx, "gateway_api_conformance")
		if err != nil {
			fmt.Printf("ERROR: conformance tests failed and failed to dump the diagnostics: %v\n", err)
		} else {
			fmt.Printf("INFO: conformance tests failed, dumped diagnostics to %s\n", output)
		}
	}
	fmt.Println("INFO: tests complete, cleaning up environment")

	cleanupResources := !test.SkipCleanup()
	fmt.Printf("INFO: clean up resources: %t\n", cleanupResources)
	// If we set the shouldCleanup flag on the conformance suite we need to wait
	// for the operator to handle Gateway finalizers.
	// If we don't do it then we'll be left with Gateways that have a deleted
	// timestamp and finalizers set but no operator running which could handle those.
	if cleanupResources {
		exitOnErr(waitForConformanceGatewaysToCleanup(ctx, clients.GatewayClient.GatewayV1()))
	}

	if existingCluster == "" && cleanupResources {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(env.Cleanup(ctx))
	}
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if existingCluster == "" {
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
