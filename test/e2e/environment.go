package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/loadimage"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/networking"
	"github.com/kong/semver/v4"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	configurationclient "github.com/kong/kubernetes-configuration/v2/pkg/clientset"

	"github.com/kong/kong-operator/internal/versions"
	"github.com/kong/kong-operator/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
)

// -----------------------------------------------------------------------------
// Testing Consts - Timeouts
// -----------------------------------------------------------------------------

const (
	webhookReadinessTimeout = 2 * time.Minute
	webhookReadinessTick    = 2 * time.Second
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster    = os.Getenv("KONG_TEST_CLUSTER")
	imageOverride      = os.Getenv("KONG_TEST_KONG_OPERATOR_IMAGE_OVERRIDE")
	imageLoad          = os.Getenv("KONG_TEST_KONG_OPERATOR_IMAGE_LOAD")
	skipClusterCleanup = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

// TestEnvironment represents a testing environment (K8s cluster) for running isolated e2e test.
type TestEnvironment struct {
	Clients     *testutils.K8sClients
	Namespace   *corev1.Namespace
	Cleaner     *clusters.Cleaner
	Environment environments.Environment
}

// TestEnvOption is a functional option for configuring a test environment.
type TestEnvOption func(opt *testEnvOptions)

type testEnvOptions struct {
	Image string
	// InstallViaKustomize makes the test environment install the operator and all the
	// dependencies via kustomize.
	// NOTE: when this is false the caller is responsible for installing (and cleaning up)
	// the operator in the test environment.
	InstallViaKustomize bool
}

// WithOperatorImage allows configuring the operator image to use in the test environment.
func WithOperatorImage(image string) TestEnvOption {
	return func(opts *testEnvOptions) {
		opts.Image = image
	}
}

// WithInstallViaKustomize makes the test environment install the operator and all the
// dependencies via kustomize.
func WithInstallViaKustomize() TestEnvOption {
	return func(opts *testEnvOptions) {
		opts.InstallViaKustomize = true
	}
}

var loggerOnce sync.Once

// AdditionalKustomizeDir is a path to additional kustomize configuration to deploy to the test cluster.
// It is applied after all configuration from this repository is applied.
var AdditionalKustomizeDir string

// CreateEnvironment creates a new independent testing environment for running isolated e2e test.
// When running with Helm, the caller is responsible for cleaning up the environment.
func CreateEnvironment(t *testing.T, ctx context.Context, opts ...TestEnvOption) TestEnvironment {
	t.Helper()

	const (
		waitTime = 1 * time.Minute
	)

	var opt testEnvOptions
	for _, o := range opts {
		o(&opt)
	}

	skipClusterCleanup = existingCluster != ""

	t.Log("configuring cluster for testing environment")
	builder := environments.NewBuilder()
	if existingCluster != "" {
		clusterParts := strings.Split(existingCluster, ":")
		require.Lenf(t, clusterParts, 2,
			"existing cluster in wrong format (%s): format is <TYPE>:<NAME> (e.g. kind:test-cluster)", existingCluster,
		)
		clusterType, clusterName := clusterParts[0], clusterParts[1]

		t.Logf("using existing %s cluster %s\n", clusterType, clusterName)
		switch clusterType {
		case string(kind.KindClusterType):
			cluster, err := kind.NewFromExisting(clusterName)
			require.NoError(t, err)
			builder.WithExistingCluster(cluster)

			if !test.IsCertManagerDisabled() {
				builder.WithAddons(certmanager.New())
			}
			if !test.IsMetalLBDisabled() {
				builder.WithAddons(metallb.New())
			}
		case string(gke.GKEClusterType):
			cluster, err := gke.NewFromExistingWithEnv(ctx, clusterName)
			require.NoError(t, err)
			builder.WithExistingCluster(cluster)
			if !test.IsCertManagerDisabled() {
				builder.WithAddons(certmanager.New())
			}
		default:
			t.Fatal(fmt.Errorf("unknown cluster type: %s", clusterType))
		}
	} else {
		t.Log("no existing cluster found, deploying using Kubernetes In Docker (KIND)")
		if !test.IsCertManagerDisabled() {
			builder.WithAddons(certmanager.New())
		}
		if !test.IsMetalLBDisabled() {
			builder.WithAddons(metallb.New())
		}
	}
	if imageLoad != "" {
		imageLoader, err := loadimage.NewBuilder().WithImage(imageLoad)
		require.NoError(t, err)
		t.Logf("loading image: %s", imageLoad)
		builder.WithAddons(imageLoader.Build())
	}

	if len(opt.Image) == 0 {
		opt.Image = getOperatorImage(t)
	}

	var kustomizeDir KustomizeDir
	if opt.InstallViaKustomize {
		kustomizeDir = PrepareKustomizeDir(t, opt.Image)
	}

	env, err := builder.Build(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		if opt.InstallViaKustomize {
			cleanupEnvironment(t, context.Background(), env, kustomizeDir.Tests())
		}
	})

	t.Logf("waiting for cluster %s and all addons to become ready", env.Cluster().Name())
	require.NoError(t, <-env.WaitForReady(ctx))

	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("initializing Kubernetes API clients")
	clients := &testutils.K8sClients{}
	clients.K8sClient = env.Cluster().Client()
	clients.OperatorClient, err = configurationclient.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)
	clients.GatewayClient, err = gatewayclient.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)

	t.Log("initializing manager client")
	loggerOnce.Do(func() {
		// A new client from package "sigs.k8s.io/controller-runtime/pkg/client" is created per execution
		// of this function (see the line below this block). It requires a logger to be set, otherwise it logs
		// "[controller-runtime] log.SetLogger(...) was never called; logs will not be displayed" with a stack trace.
		// Since setting logger is a package level operation not safe for concurrent use, ensure it is set
		// only once.
		ctrllog.SetLogger(zap.New(func(o *zap.Options) {
			o.DestWriter = io.Discard
		}))
	})
	clients.MgrClient, err = client.New(env.Cluster().Config(),
		client.Options{
			Scheme: scheme.Get(),
		},
	)
	require.NoError(t, err)

	if opt.InstallViaKustomize {
		t.Logf("deploying Gateway APIs CRDs from %s", testutils.GatewayExperimentalCRDsKustomizeURL)
		require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), testutils.GatewayExperimentalCRDsKustomizeURL))

		kicCRDsKustomizeURL := getCRDsKustomizeURLForKIC(t, versions.DefaultControlPlaneVersion)
		t.Logf("deploying KIC CRDs from %s", kicCRDsKustomizeURL)
		require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kicCRDsKustomizeURL, "--server-side"))

		t.Log("creating system namespaces and serviceaccounts")
		require.NoError(t, clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))

		t.Logf("deploying operator CRDs to test cluster via kustomize (%s)", kustomizeDir.CRD())
		require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kustomizeDir.CRD(), "--server-side"))

		t.Logf("deploying operator to test cluster via kustomize (%s)", kustomizeDir.Tests())
		require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kustomizeDir.Tests(), "--server-side"))

		if AdditionalKustomizeDir != "" {
			t.Logf("deploying additional configuration to test cluster via kustomize (%s)", AdditionalKustomizeDir)
			require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), AdditionalKustomizeDir))
		} else {
			t.Log("no additional additional configuration provided")
		}

		t.Log("waiting for operator deployment to complete")
		require.NoError(t, waitForOperatorDeployment(t, ctx, "kong-system", clients.K8sClient, waitTime))

		if test.IsWebhookEnabled() {
			t.Log("waiting for operator webhook service to be connective")
			require.Eventually(t, waitForOperatorWebhookEventually(t, ctx, clients.K8sClient),
				webhookReadinessTimeout, webhookReadinessTick)
		}
	} else {
		t.Log("not deploying operator to test cluster via kustomize")
	}

	t.Log("environment is ready, starting tests")

	return TestEnvironment{
		Clients:     clients,
		Namespace:   namespace,
		Cleaner:     cleaner,
		Environment: env,
	}
}

// getCRDsKustomizeURLForKIC returns the Kubernetes Ingress Controller CRDs Kustomization URL for a given version.
func getCRDsKustomizeURLForKIC(t *testing.T, version string) string {
	v, err := semver.Parse(version)
	require.NoError(t, err)
	const kicCRDsKustomizeURLTemplate = "https://github.com/Kong/kubernetes-ingress-controller/config/crd?ref=v%s"
	return fmt.Sprintf(kicCRDsKustomizeURLTemplate, v)
}

func cleanupEnvironment(t *testing.T, ctx context.Context, env environments.Environment, kustomizePath string) {
	t.Helper()

	if env == nil {
		return
	}

	if skipClusterCleanup {
		t.Logf("cleaning up operator manifests using kustomize path: %s", kustomizePath)
		assert.NoError(t, clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), kustomizePath))
		return
	}

	t.Logf("cleaning up testing cluster and environment %q", env.Name())
	assert.NoError(t, env.Cleanup(ctx))
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

type deploymentAssertOptions func(*appsv1.Deployment) bool

func deploymentAssertConditions(t *testing.T, conds ...appsv1.DeploymentCondition) deploymentAssertOptions {
	t.Helper()

	return func(deployment *appsv1.Deployment) bool {
		return lo.EveryBy(conds, func(cond appsv1.DeploymentCondition) bool {
			if !lo.ContainsBy(deployment.Status.Conditions, func(c appsv1.DeploymentCondition) bool {
				return c.Type == cond.Type &&
					c.Status == cond.Status &&
					c.Reason == cond.Reason
			}) {
				t.Logf("Deployment %s/%s does not have condition %#v", deployment.Namespace, deployment.Name, cond)
				t.Logf("Deployment %s/%s current status: %#v", deployment.Namespace, deployment.Name, deployment.Status)
				return false
			}
			return true
		})
	}
}

func waitForOperatorDeployment(
	t *testing.T,
	ctx context.Context,
	ns string,
	k8sClient *kubernetes.Clientset,
	waitTime time.Duration,
	opts ...deploymentAssertOptions,
) error {
	t.Helper()

	timer := time.NewTimer(waitTime)
	defer timer.Stop()
	pollTimer := time.NewTicker(time.Second)
	defer pollTimer.Stop()

	for {
		select {
		case <-timer.C:
			logOperatorPodLogs(t, ctx, k8sClient, ns)
			return fmt.Errorf("timed out waiting for operator deployment in namespace %s", ns)
		case <-ctx.Done():
			logOperatorPodLogs(t, t.Context(), k8sClient, ns)
			return ctx.Err()
		case <-pollTimer.C:
			listOpts := metav1.ListOptions{
				// NOTE: This is a common label used by:
				// - kustomize https://github.com/kong/kong-operator/blob/f98ef9358078ac100e143ab677a9ca836d0222a0/config/manager/manager.yaml#L15
				// - helm https://github.com/Kong/charts/blob/4968b34ae7c252ab056b37cc137eaeb7a071e101/charts/gateway-operator/templates/deployment.yaml#L5-L6
				//
				// As long as kustomize is used for tests let's use this label selector.
				LabelSelector: "app.kubernetes.io/name=kong-operator",
			}
			deploymentList, err := k8sClient.AppsV1().Deployments(ns).List(ctx, listOpts)
			if err != nil {
				return err
			}
			if len(deploymentList.Items) == 0 {
				t.Logf("No operator deployment found in namespace %s", ns)
				continue
			}

			deployment := &deploymentList.Items[0]

			if deployment.Status.AvailableReplicas <= 0 {
				t.Logf("Deployment %s/%s has no AvailableReplicas", ns, deployment.Name)
				continue
			}

			for _, opt := range opts {
				if !opt(deployment) {
					continue
				}
			}
			return nil
		}
	}
}

func logOperatorPodLogs(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, ns string) {
	t.Helper()

	pods, err := k8sClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=kong-operator",
	})
	if err != nil {
		t.Logf("Failed to list operator pods in namespace %s: %v", ns, err)
		return
	}

	if len(pods.Items) == 0 {
		t.Logf("No operator pod found in namespace %s", ns)
		return
	}

	result := k8sClient.CoreV1().Pods(ns).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{
		Container:                    "manager",
		InsecureSkipTLSVerifyBackend: true,
	}).Do(ctx)

	if result.Error() != nil {
		t.Logf("Failed to get logs from operator pod %s/%s: %v", ns, pods.Items[0].Name, result.Error())
		return
	}

	b, err := result.Raw()
	if err != nil {
		t.Logf("Failed to read logs from operator pod %s/%s: %v", ns, pods.Items[0].Name, err)
		return
	}

	t.Logf("Operator pod logs:\n%s", string(b))
}

func waitForOperatorWebhookEventually(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset) func() bool {
	return func() bool {
		if err := waitForOperatorWebhook(ctx, k8sClient); err != nil {
			t.Logf("failed to wait for operator webhook: %v", err)
			return false
		}

		t.Log("operator webhook ready")
		return true
	}
}

func waitForOperatorWebhook(ctx context.Context, k8sClient *kubernetes.Clientset) error {
	webhookServiceNamespace := "kong-system"
	webhookServiceName := "gateway-operator-validating-webhook"
	webhookServicePort := 443
	return networking.WaitForConnectionOnServicePort(ctx, k8sClient, webhookServiceNamespace, webhookServiceName, webhookServicePort, 10*time.Second)
}
