//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/loadimage"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/clientset"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingClusterName = os.Getenv("KONG_TEST_CLUSTER")
	imageOverride       = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE")
	imageLoad           = os.Getenv("KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD")
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
	ctx    context.Context
	cancel context.CancelFunc
	env    environments.Environment

	k8sClient      *kubernetes.Clientset
	operatorClient *clientset.Clientset
	mgrClient      client.Client
)

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	var skipClusterCleanup bool
	var existingCluster clusters.Cluster
	var err error

	fmt.Println("INFO: setting up test cluster")
	if existingClusterName != "" {
		existingCluster, err = kind.NewFromExisting(existingClusterName)
		exitOnErr(err)
		skipClusterCleanup = true
		fmt.Printf("INFO: using existing kind cluster (name: %s)\n", existingCluster.Name())
	}

	fmt.Println("INFO: setting up test environment")
	envBuilder := environments.NewBuilder()
	if existingCluster != nil {
		envBuilder.WithExistingCluster(existingCluster)
	}

	addons := []clusters.Addon{
		metallb.New(),
	}

	if imageLoad != "" {
		imageLoader, err := loadimage.NewBuilder().WithImage(imageLoad)
		exitOnErr(err)
		fmt.Println("INFO: load image", imageLoad)
		addons = append(addons, imageLoader.Build())
	}

	env, err = envBuilder.WithAddons(addons...).Build(ctx)
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	exitOnErr(<-env.WaitForReady(ctx))

	fmt.Println("INFO: initializing Kubernetes API clients")
	k8sClient = env.Cluster().Client()
	operatorClient, err = clientset.NewForConfig(env.Cluster().Config())
	exitOnErr(err)
	mgrClient, err = client.New(env.Cluster().Config(), client.Options{})
	exitOnErr(err)

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))

	exitOnErr(setOperatorImage())

	fmt.Println("INFO: deploying operator to test cluster via kustomize")
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), kustomizationDir))

	fmt.Println("INFO: waiting for operator deployment to complete")
	exitOnErr(waitForOperatorDeployment())

	fmt.Println("INFO: environment is ready, starting tests")
	code := m.Run()

	if skipClusterCleanup {
		fmt.Println("INFO: cleaning up operator manifests")
		exitOnErr(clusters.KustomizeDeleteForCluster(ctx, env.Cluster(), kustomizationDir))
	} else {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(env.Cleanup(ctx))
	}

	exitOnErr(restoreKustomizationFile())

	os.Exit(code)
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if env != nil {
			env.Cleanup(ctx) //nolint:errcheck
		}
		fmt.Printf("ERROR: %s\n", err.Error())
		os.Exit(1)
	}
}

func waitForOperatorDeployment() error {
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
