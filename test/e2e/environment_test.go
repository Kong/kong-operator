//go:build e2e_tests
// +build e2e_tests

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
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/clientset"
	"github.com/kong/gateway-operator/test/consts"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster = os.Getenv("KONG_TEST_CLUSTER")
	imageOverride   = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE")
	imageLoad       = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD")
)

// -----------------------------------------------------------------------------
// Testing Vars - path of kustomization directories and files
// -----------------------------------------------------------------------------

var (
	kustomizationDir  = "../../config/default"
	kustomizationFile = kustomizationDir + "/kustomization.yaml"
	// backupKustomizationFile is used to save the original kustomization file if we modified it.
	// iIf the kustomization file is changed multiple times,
	// only the content before the first change should be used as backup to keep the content as same as the origin.
	backupKustomizationFile = ""
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

var (
	skipClusterCleanup bool
)

// -----------------------------------------------------------------------------
// Testing Structs - Testing Environment
// -----------------------------------------------------------------------------

type k8sClients struct {
	k8sClient      *kubernetes.Clientset
	operatorClient *clientset.Clientset
	mgrClient      client.Client
}

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func createEnvironment(t *testing.T, ctx context.Context) (environments.Environment, *k8sClients) {
	skipClusterCleanup = existingCluster != ""

	fmt.Println("INFO: configuring cluster for testing environment")
	builder := environments.NewBuilder()
	if existingCluster != "" {
		clusterParts := strings.Split(existingCluster, ":")
		if len(clusterParts) != 2 {
			t.Fatal(fmt.Errorf("existing cluster in wrong format (%s): format is <TYPE>:<NAME> (e.g. kind:test-cluster)", existingCluster))
		}
		clusterType, clusterName := clusterParts[0], clusterParts[1]

		fmt.Printf("INFO: using existing %s cluster %s\n", clusterType, clusterName)
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
		fmt.Println("INFO: no existing cluster found, deploying using Kubernetes In Docker (KIND)")
		builder.WithAddons(metallb.New())
	}
	if imageLoad != "" {
		imageLoader, err := loadimage.NewBuilder().WithImage(imageLoad)
		require.NoError(t, err)
		fmt.Println("INFO: load image", imageLoad)
		builder.WithAddons(imageLoader.Build())
	}
	var err error
	env, err := builder.Build(ctx)
	require.NoError(t, err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	require.NoError(t, <-env.WaitForReady(ctx))

	fmt.Println("INFO: initializing Kubernetes API clients")
	clients := &k8sClients{}
	clients.k8sClient = env.Cluster().Client()
	clients.operatorClient, err = clientset.NewForConfig(env.Cluster().Config())
	require.NoError(t, err)
	clients.mgrClient, err = client.New(env.Cluster().Config(), client.Options{})
	require.NoError(t, err)

	fmt.Printf("deploying Gateway APIs CRDs from %s\n", consts.GatewayCRDsKustomizeURL)
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), consts.GatewayCRDsKustomizeURL))

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	require.NoError(t, clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))

	require.NoError(t, setOperatorImage())

	fmt.Println("INFO: deploying operator to test cluster via kustomize")
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kustomizationDir))

	fmt.Println("INFO: waiting for operator deployment to complete")
	require.NoError(t, waitForOperatorDeployment(ctx, clients.k8sClient))

	fmt.Println("INFO: waiting for operator webhook service to be connective")
	require.NoError(t, waitForOperatorWebhook(ctx, clients.k8sClient))

	fmt.Println("INFO: environment is ready, starting tests")

	require.NoError(t, restoreKustomizationFile())

	return env, clients
}

func cleanupEnvironment(ctx context.Context, env environments.Environment) error {
	if env == nil {
		return nil
	}
	if skipClusterCleanup {
		fmt.Println("INFO: cleaning up operator manifests")
		return clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), kustomizationDir)
	}

	fmt.Println("INFO: cleaning up testing cluster and environment")
	return env.Cleanup(ctx)
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func waitForOperatorDeployment(ctx context.Context, k8sClient *kubernetes.Clientset) error {
	ready := false
	for !ready {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			deployment, err := k8sClient.AppsV1().Deployments("kong-system").Get(ctx, "gateway-operator-controller-manager", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if deployment.Status.AvailableReplicas >= 1 {
				ready = true
			}
		}
	}
	return nil
}

func waitForOperatorWebhook(ctx context.Context, k8sClient *kubernetes.Clientset) error {
	webhookServiceNamespace := "kong-system"
	webhookServiceName := "gateway-operator-validating-webhook"
	webhookServicePort := 443
	return networking.WaitForConnectionOnServicePort(ctx, k8sClient, webhookServiceNamespace, webhookServiceName, webhookServicePort, 10*time.Second)
}
