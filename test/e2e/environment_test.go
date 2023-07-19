//go:build e2e_tests

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/loadimage"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/networking"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/clientset"
	"github.com/kong/gateway-operator/test/helpers"
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
	imageOverride      = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE")
	imageLoad          = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD")
	skipClusterCleanup = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
)

// -----------------------------------------------------------------------------
// Testing Consts - paths of kustomization directories and files
// -----------------------------------------------------------------------------

const (
	// testsKustomizationPath is a relative path to tests kustomization directory.
	testsKustomizationPath = "config/tests/"
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

type testEnvironment struct {
	Clients     *testutils.K8sClients
	Namespace   *corev1.Namespace
	Cleaner     *clusters.Cleaner
	Environment environments.Environment
}

type TestEnvOption func(opt *TestEnvOptions)

type TestEnvOptions struct {
	Image string
}

func WithOperatorImage(image string) TestEnvOption {
	return func(opts *TestEnvOptions) {
		opts.Image = image
	}
}

func createEnvironment(t *testing.T, ctx context.Context, opts ...TestEnvOption) testEnvironment {
	t.Helper()
	var opt TestEnvOptions
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
			builder.WithAddons(metallb.New())
		case string(gke.GKEClusterType):
			cluster, err := gke.NewFromExistingWithEnv(ctx, clusterName)
			require.NoError(t, err)
			builder.WithExistingCluster(cluster)
		default:
			t.Fatal(fmt.Errorf("unknown cluster type: %s", clusterType))
		}
	} else {
		t.Log("no existing cluster found, deploying using Kubernetes In Docker (KIND)")
		builder.WithAddons(metallb.New())
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
	kustomizationDir := prepareKustomizeDir(t, opt.Image)

	env, err := builder.Build(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupEnvironment(t, context.Background(), env, kustomizationDir)
	})

	t.Logf("waiting for cluster %s and all addons to become ready", env.Cluster().Name())
	require.NoError(t, <-env.WaitForReady(ctx))

	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("initializing Kubernetes API clients")
	clients := &testutils.K8sClients{}
	clients.K8sClient = env.Cluster().Client()
	clients.OperatorClient, err = clientset.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)
	clients.GatewayClient, err = gatewayclient.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)

	t.Log("intializing manager client")
	clients.MgrClient, err = client.New(env.Cluster().Config(), client.Options{})
	require.NoError(t, err)

	require.NoError(t, gatewayv1beta1.AddToScheme(clients.MgrClient.Scheme()))
	require.NoError(t, operatorv1alpha1.AddToScheme(clients.MgrClient.Scheme()))

	t.Logf("deploying Gateway APIs CRDs from %s", testutils.GatewayExperimentalCRDsKustomizeURL)
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), testutils.GatewayExperimentalCRDsKustomizeURL))

	t.Logf("deploying KIC CRDs from %s", "./../../kubernetes-ingress-controller/config/crd/")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), "./../../kubernetes-ingress-controller/config/crd/"))

	t.Log("creating system namespaces and serviceaccounts")
	require.NoError(t, clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))

	t.Log("deploying operator to test cluster via kustomize")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kustomizationDir, "--server-side", "-v5"))

	t.Log("waiting for operator deployment to complete")
	require.NoError(t, waitForOperatorDeployment(ctx, clients.K8sClient))

	t.Log("waiting for operator webhook service to be connective")
	require.Eventually(t, waitForOperatorWebhookEventually(t, ctx, clients.K8sClient),
		webhookReadinessTimeout, webhookReadinessTick)

	t.Log("environment is ready, starting tests")

	return testEnvironment{
		Clients:     clients,
		Namespace:   namespace,
		Cleaner:     cleaner,
		Environment: env,
	}
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

type DeploymentAssertOptions func(*appsv1.Deployment) bool

func DeploymentAssertConditions(conds ...appsv1.DeploymentCondition) DeploymentAssertOptions {
	return func(deployment *appsv1.Deployment) bool {
		return lo.EveryBy(conds, func(cond appsv1.DeploymentCondition) bool {
			return lo.ContainsBy(deployment.Status.Conditions, func(c appsv1.DeploymentCondition) bool {
				return c.Type == cond.Type &&
					c.Status == cond.Status &&
					c.Reason == cond.Reason
			})
		})
	}
}

func waitForOperatorDeployment(ctx context.Context, k8sClient *kubernetes.Clientset, opts ...DeploymentAssertOptions) error {
outer:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			deployment, err := k8sClient.AppsV1().Deployments("kong-system").Get(ctx, "gateway-operator-controller-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if deployment.Status.AvailableReplicas <= 0 {
				continue
			}

			for _, opt := range opts {
				if !opt(deployment) {
					continue outer
				}
			}
			return nil
		}
	}
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
