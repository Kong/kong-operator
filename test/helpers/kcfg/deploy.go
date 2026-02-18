package kcfg

import (
	"context"
	"fmt"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/kong/kong-operator/v2/test"
)

// DeployKubernetesConfiguration deploys the common Kubernetes configuration
// needed for tests to the provided cluster - namespace, RBAC, CRDs, etc.
func DeployKubernetesConfiguration(ctx context.Context, cluster clusters.Cluster) error {
	const systemNS = "kong-system"
	fmt.Printf("INFO: creating namespaces %s (for controller)\n", systemNS)
	if err := clusters.CreateNamespace(ctx, cluster, systemNS); err != nil {
		return err
	}

	for _, cfg := range []string{
		rbacBase,
		rbacRole,
		validatingPolicies,
	} {
		fmt.Printf("INFO: deploying Kubernetes configuration: %s\n", cfg)
		if err := clusters.KustomizeDeployForCluster(ctx, cluster, cfg); err != nil {
			return err
		}
	}

	if !test.IsInstallingCRDsDisabled() {
		fmt.Println("INFO: deploying CRDs to test cluster")
		if err := deployCRDs(ctx, cluster); err != nil {
			return err
		}
	}
	return nil
}

// deployCRDs deploys the CRDs commonly used in tests.
func deployCRDs(ctx context.Context, cluster clusters.Cluster) error {
	kubectlFlags := []string{"--server-side", "-v5"}

	// CRDs for gateway APIs.
	fmt.Printf("INFO: deploying Gateway API CRDs: %s\n", GatewayAPIExperimentalCRDsPath())
	if err := clusters.KustomizeDeployForCluster(ctx, cluster, GatewayAPIExperimentalCRDsPath(), kubectlFlags...); err != nil {
		return err
	}

	// Local CRDs for Kong Operator.
	for _, p := range []string{
		KongOperatorCRDsPath(),
		IngressControllerIncubatorCRDsPath(),
	} {
		fmt.Printf("INFO: deploying local CRDs: %s\n", p)
		if err := clusters.KustomizeDeployForCluster(ctx, cluster, p, kubectlFlags...); err != nil {
			return fmt.Errorf("failed installing CRDs from %s: %w", p, err)
		}
	}

	apiextClient, err := apiextclient.NewForConfig(cluster.Config())
	if err != nil {
		return err
	}

	// Some kind of a canary to ensure that the CRDs are actually installed before proceeding.
	for _, crd := range []string{
		"gateways.gateway.networking.k8s.io",                         // GWAPI CRD
		"dataplanes.gateway-operator.konghq.com",                     // kong-operator
		"kongservicefacades.incubator.ingress-controller.konghq.com", // ingress-controller-incubator
	} {
		if err := retry.OnError(
			retry.DefaultRetry,
			apierrors.IsNotFound,
			func() error {
				_, err := apiextClient.CustomResourceDefinitions().Get(ctx, crd, metav1.GetOptions{})
				return err
			},
		); err != nil {
			return err
		}
	}

	return nil
}
