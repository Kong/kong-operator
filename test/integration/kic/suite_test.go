//go:build integration_tests

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/blang/semver/v4"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/scheme"
	"github.com/kong/kong-operator/ingress-controller/test"
	"github.com/kong/kong-operator/ingress-controller/test/testenv"
	testutils "github.com/kong/kong-operator/ingress-controller/test/util"
	"github.com/kong/kong-operator/test/integration/kic/consts"
)

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	var code int
	defer func() {
		os.Exit(code)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Logger needs to be configured before anything else happens.
	// This is because the controller manager has a timeout for
	// logger initialization, and if the logger isn't configured
	// after 30s from the start of controller manager package init function,
	// the controller manager will set up a no op logger and continue.
	// The logger cannot be configured after that point.
	logger, logOutput, err := testutils.SetupLoggers("trace", "text")
	if err != nil {
		ExitOnErrWithCode(ctx, fmt.Errorf("failed to setup loggers: %w", err), consts.ExitCodeCantCreateLogger)
	}
	if logOutput != "" {
		fmt.Printf("INFO: writing manager logs to %s\n", logOutput)
	}

	fmt.Println("INFO: setting up test environment")
	kongbuilder, _, err := GenerateKongBuilder(ctx)
	ExitOnErrWithCode(ctx, err, consts.ExitCodeEnvSetupFailed)
	if testenv.KongImage() != "" && testenv.KongTag() != "" {
		fmt.Printf("INFO: custom kong image specified via env: %s:%s\n", testenv.KongImage(), testenv.KongTag())
	}
	// add env for vaults.
	KongWithProxyEnvVar(kongbuilder, "vault_test_add_header_1", "h1:v1")

	// Pin the Helm chart version.
	KongWithHelmChartVersion(kongbuilder, testenv.KongHelmChartVersion())

	kongAddon := kongbuilder.Build()
	builder := environments.NewBuilder().WithAddons(kongAddon)

	fmt.Println("INFO: configuring cluster for testing environment")
	if existingCluster := testenv.ExistingClusterName(); existingCluster != "" {
		if testenv.ClusterVersion() != "" {
			ExitOnErrWithCode(ctx, fmt.Errorf("can't flag cluster version & provide an existing cluster at the same time"), consts.ExitCodeIncompatibleOptions)
		}
		clusterParts := strings.Split(existingCluster, ":")
		if len(clusterParts) != 2 {
			ExitOnErrWithCode(ctx, fmt.Errorf("existing cluster in wrong format (%s): format is <TYPE>:<NAME> (e.g. kind:test-cluster)", existingCluster), consts.ExitCodeCantUseExistingCluster)
		}
		clusterType, clusterName := clusterParts[0], clusterParts[1]

		fmt.Printf("INFO: using existing %s cluster %s\n", clusterType, clusterName)
		switch clusterType {
		case string(kind.KindClusterType):
			cluster, err := kind.NewFromExisting(clusterName)
			ExitOnErr(ctx, err)
			builder.WithExistingCluster(cluster)
			builder.WithAddons(metallb.New())
		case string(gke.GKEClusterType):
			cluster, err := gke.NewFromExistingWithEnv(ctx, clusterName)
			ExitOnErr(ctx, err)
			builder.WithExistingCluster(cluster)
		default:
			ExitOnErrWithCode(ctx, fmt.Errorf("unknown cluster type: %s", clusterType), consts.ExitCodeCantUseExistingCluster)
		}
	} else {
		fmt.Println("INFO: no existing cluster found, deploying using Kubernetes In Docker (KIND)")

		builder.WithAddons(metallb.New())

		if testenv.ClusterVersion() != "" {
			clusterVersion, err := semver.Parse(strings.TrimPrefix(testenv.ClusterVersion(), "v"))
			ExitOnErr(ctx, err)

			fmt.Printf("INFO: build a new KIND cluster with version %s\n", clusterVersion.String())
			builder.WithKubernetesVersion(clusterVersion)
		}
	}

	fmt.Println("INFO: building test environment")
	env, err = builder.Build(ctx)
	ExitOnErr(ctx, err)

	cleaner := clusters.NewCleaner(env.Cluster(), scheme.Get())
	defer func() {
		if err := cleaner.Cleanup(ctx); err != nil {
			fmt.Printf("ERROR: failed cleaning up the cluster: %v\n", err)
		}
	}()

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	envReadyCtx, envReadyCancel := context.WithTimeout(ctx, testenv.EnvironmentReadyTimeout())
	defer envReadyCancel()
	ExitOnErr(ctx, <-env.WaitForReady(envReadyCtx))

	fmt.Println("INFO: collecting urls from the kong proxy deployment")
	proxyHTTPURL, err = kongAddon.ProxyHTTPURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)
	proxyHTTPSURL, err = kongAddon.ProxyHTTPSURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)
	proxyAdminURL, err = kongAddon.ProxyAdminURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)
	proxyTCPURL, err = kongAddon.ProxyTCPURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)
	proxyTLSURL, err = kongAddon.ProxyTLSURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)
	proxyUDPURL, err = kongAddon.ProxyUDPURL(ctx, env.Cluster())
	ExitOnErr(ctx, err)

	ExitOnErr(
		ctx,
		retry.Do(
			func() error {
				reqCtx, cancel := context.WithTimeout(ctx, test.RequestTimeout)
				defer cancel()
				kongVersion, err := ValidateMinimalSupportedKongVersion(reqCtx, proxyAdminURL, consts.KongTestPassword)
				if err != nil {
					return err
				}
				fmt.Printf("INFO: using Kong instance (version: %q) reachable at %s\n", kongVersion, proxyAdminURL)
				return nil
			},
			retry.OnRetry(
				func(n uint, err error) {
					fmt.Printf("WARNING: try to get Kong Gateway version attempt %d/10 - error: %s\n", n+1, err)
				},
			),
			retry.LastErrorOnly(true), retry.RetryIf(func(err error) bool {
				return !errors.As(err, &TooOldKongGatewayError{})
			}),
		))

	if v := os.Getenv("KONG_BRING_MY_OWN_KIC"); v == "true" {
		fmt.Println("WARNING: caller indicated that they will manage their own controller")
	} else {
		fmt.Println("INFO: configuring feature gates")
		featureGates := make(managercfg.FeatureGates)
		for _, fg := range strings.Split(testenv.ControllerFeatureGates(), ",") {
			fg = strings.TrimSpace(fg)
			if fg == "" {
				continue
			}
			fgParts := strings.Split(fg, "=")
			if len(fgParts) != 2 {
				ExitOnErrWithCode(ctx, fmt.Errorf("feature gate %q not in key=value format", fg), consts.ExitCodeIncompatibleOptions)
			}
			fgName, fgValue := fgParts[0], fgParts[1]
			if _, ok := managercfg.GetFeatureGatesDefaults()[fgName]; !ok {
				ExitOnErrWithCode(ctx, fmt.Errorf("unknown feature gate %q (see %s for available feature gates)", fgName, managercfg.DocsURL), consts.ExitCodeIncompatibleOptions)
			}
			if fgValue != "true" && fgValue != "false" {
				ExitOnErrWithCode(ctx, fmt.Errorf("feature gate %q must be true or false, got %q", fgName, fgValue), consts.ExitCodeIncompatibleOptions)
			}
			featureGates[fgName] = fgValue == "true"
		}

		fmt.Printf("INFO: feature gates enabled: %v\n", featureGates)

		fmt.Println("Preparing the environment to run the controller manager")
		ExitOnErr(ctx, testutils.PrepareClusterForRunningControllerManager(ctx, env.Cluster()))

		fmt.Println("INFO: starting the controller manager")
		cancel, err := testutils.DeployControllerManagerForCluster(ctx, logger, env.Cluster(), kongAddon, func(c *managercfg.Config) {
			c.IngressClassName = consts.IngressClass
			c.EnableProfiling = true
			c.EnableConfigDumps = true
			c.DumpSensitiveConfig = true
			c.LogLevel = "trace" // not used, as controller logger is configured separately
			c.AnonymousReports = false
			c.FeatureGates = featureGates
			c.LeaderElectionNamespace = kongAddon.Namespace()
			// Leader election is irrelevant for single-instance tests. We should effectively always be the leader. However,
			// controller-runtime operates an internal leadership deadline and will abort if it cannot update leadership
			// within a certain number of seconds. Pausing certain segments manager in a debugger can exceed this deadline,
			// so elections are disabled in integration tests for convenience.
			c.LeaderElectionForce = managercfg.LeaderElectionDisabled
			// Increase Admin API readiness tolerance for flakier CI environments.
			// The default timeout is 5s; in CI the admin API may be slower to accept connections.
			// We also ensure the reconciliation interval remains larger than the timeout per validation rules.
			c.GatewayDiscoveryReadinessCheckTimeout = 15 * time.Second
			c.GatewayDiscoveryReadinessCheckInterval = 20 * time.Second
		})
		defer cancel()
		ExitOnErr(ctx, err)
	}

	gatewayClient, err := gatewayclient.NewForConfig(env.Cluster().Config())
	ExitOnErr(ctx, err)

	fmt.Println("INFO: Deploying the default GatewayClass")
	gwc, err := DeployGatewayClass(ctx, gatewayClient, unmanagedGatewayClassName)
	ExitOnErr(ctx, err)
	cleaner.Add(gwc)

	fmt.Printf("INFO: Deploying the controller's IngressClass %q\n", consts.IngressClass)
	ExitOnErr(ctx, CreateIngressClass(ctx, consts.IngressClass, env.Cluster().Client()))
	defer func() {
		// deleting this directly instead of adding it to the cleaner because
		// the cleaner always gets a 404 on it for unknown reasons
		_ = env.Cluster().Client().NetworkingV1().IngressClasses().Delete(ctx, consts.IngressClass, metav1.DeleteOptions{})
	}()

	if os.Getenv("TEST_RUN_INVALID_CONFIG_CASES") == "true" {
		fmt.Println("INFO: run tests with invalid configurations")
		fmt.Println("WARN: should run these cases separately to prevent config being affected by invalid cases")
		runInvalidConfigTests = true
	}

	clusterVersion, err := env.Cluster().Version()
	ExitOnErr(ctx, err)

	fmt.Printf("INFO: testing environment is ready KUBERNETES_VERSION=(%v): running tests\n", clusterVersion)
	code = m.Run()

	if testenv.IsCI() {
		fmt.Printf("INFO: running in ephemeral CI environment, skipping cluster %s teardown\n", env.Cluster().Name())
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), test.EnvironmentCleanupTimeout)
		defer cancel()
		ExitOnErr(ctx, RemoveCluster(ctx, env.Cluster()))
	}
}
