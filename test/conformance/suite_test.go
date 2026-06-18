package conformance

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/typed/apis/v1"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overridable
// -----------------------------------------------------------------------------

// conformanceInfraNamespace is the namespace where conformance
// test suite creates its resources.
const conformanceInfraNamespace = "gateway-conformance-infra"

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

	controllerNamespace := testutils.DefaultControllerConfigForTests().ControllerNamespace

	cleanupControllerResources, err := helpers.SetupControllerOperatorResources(ctx, controllerNamespace, clients.MgrClient)
	exitOnErr(err)
	defer cleanupControllerResources()

	fmt.Println("INFO: Deploying all required Kubernetes Configuration (RBAC, CRDs, etc.) for the operator")
	exitOnErr(kcfg.DeployKubernetesConfiguration(ctx, env.Cluster()))

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
		logf := func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
		exitOnErr(waitForConformanceGatewaysToCleanup(ctx, clients.GatewayClient.GatewayV1(), logf))
		exitOnErr(waitForConformanceKonnectGatewayControlPlanesToCleanup(ctx, logf))
		// Gateways and KonnectGatewayControlPlanes are not the only resources the
		// operator finalizes. Deleting them cascades to Konnect entity CRs
		// (KongService, KongRoute, KongUpstream, KongTarget, ...) created in the
		// conformance namespaces. Those carry their own finalizers, and if the
		// operator's controller manager is stopped (ctx cancelled below) before
		// it removes them, the entities are left dangling in the cluster and in
		// Konnect. A namespace cannot finish terminating while it still holds
		// finalizer-bearing resources, so waiting for the conformance namespaces
		// to fully disappear keeps the operator alive until every entity it owns
		// has been cleaned up.
		exitOnErr(waitForConformanceNamespacesToCleanup(ctx, clients.MgrClient, logf))
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
	// Disable validating webhook, since some tests checks statuses
	// for invalid resources, that would be blocked by the webhook.
	cfg.ValidatingWebhookEnabled = false

	startedChan := make(chan struct{})
	go func() {
		exitOnErr(manager.Run(cfg, scheme.Get(), manager.SetupControllers, startedChan, metadata))
	}()

	return startedChan
}

func waitForConformanceGatewaysToCleanup(ctx context.Context, gw gwapiv1.GatewayV1Interface, logf func(string, ...any)) error {
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
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to list Gateways in %s namespace during cleanup: %w", conformanceInfraNamespace, err)
			}
			for _, g := range gws.Items {
				logf("Gateway %s has deletion timestamp %v and finalizers %v", g.Name, g.DeletionTimestamp, g.Finalizers)
			}
			if len(gws.Items) == 0 {
				return nil
			}
			gatewayRemaining = len(gws.Items)
		}
	}
}

func waitForConformanceKonnectGatewayControlPlanesToCleanup(ctx context.Context, logf func(string, ...any)) error {
	var (
		ticker                 = time.NewTicker(100 * time.Millisecond)
		controlPlanesRemaining = 0
	)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("conformance cleanup failed (%d KonnectGatewayControlPlanes remain): %w", controlPlanesRemaining, ctx.Err())
		case <-ticker.C:
			var controlPlaneList konnectv1alpha2.KonnectGatewayControlPlaneList
			if err := clients.MgrClient.List(ctx, &controlPlaneList, client.InNamespace(conformanceInfraNamespace)); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to list KonnectGatewayControlPlanes in %s namespace during cleanup: %w", conformanceInfraNamespace, err)
			}

			for _, cp := range controlPlaneList.Items {
				logf("KonnectGatewayControlPlane %s has deletion timestamp %v and finalizers %v",
					cp.Name, cp.DeletionTimestamp, cp.Finalizers,
				)
			}

			if len(controlPlaneList.Items) == 0 {
				return nil
			}
			controlPlanesRemaining = len(controlPlaneList.Items)
		}
	}
}

// conformanceNamespacePrefix is the prefix shared by every namespace the
// conformance suite creates (gateway-conformance-infra, -app-backend,
// -web-backend).
const conformanceNamespacePrefix = "gateway-conformance-"

// conformanceNamespaceCleanupTimeout bounds how long we keep the operator alive
// waiting for the conformance namespaces to terminate, so a genuinely stuck
// finalizer surfaces as a failure instead of hanging the suite forever.
const conformanceNamespaceCleanupTimeout = 5 * time.Minute

// waitForConformanceNamespacesToCleanup blocks until no namespace with the
// conformance prefix exists anymore. Because a namespace stays in Terminating
// while it still contains finalizer-bearing resources, this guarantees that all
// operator-owned Konnect entity CRs (KongService, KongRoute, KongUpstream,
// KongTarget, ...) created in those namespaces have been fully reconciled away
// before the controller manager is shut down.
func waitForConformanceNamespacesToCleanup(ctx context.Context, cl client.Client, logf func(string, ...any)) error {
	ctx, cancel := context.WithTimeout(ctx, conformanceNamespaceCleanupTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var remaining []string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("conformance cleanup failed (namespaces still terminating: %v): %w", remaining, ctx.Err())
		case <-ticker.C:
			var nsList corev1.NamespaceList
			if err := cl.List(ctx, &nsList); err != nil {
				return fmt.Errorf("failed to list namespaces during cleanup: %w", err)
			}

			remaining = remaining[:0]
			for i := range nsList.Items {
				if strings.HasPrefix(nsList.Items[i].Name, conformanceNamespacePrefix) {
					remaining = append(remaining, nsList.Items[i].Name)
				}
			}
			if len(remaining) == 0 {
				return nil
			}
			logf("waiting for conformance namespaces to finish terminating (operator still cleaning up owned entities): %v", remaining)
		}
	}
}
