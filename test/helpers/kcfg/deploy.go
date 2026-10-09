package kcfg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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

// errCRDNotEstablished is returned when a CRD exists but its Established condition is not yet
// True, i.e. its REST API is not yet being served.
var errCRDNotEstablished = errors.New("CRD not established")

// isCRDEstablished returns true if the CRD exists and has its Established condition set to True.
func isCRDEstablished(ctx context.Context, client *apiextclient.ApiextensionsV1Client, name string) (bool, error) {
	crd, err := client.CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	for _, c := range crd.Status.Conditions {
		if c.Type == apiextv1.Established && c.Status == apiextv1.ConditionTrue {
			return true, nil
		}
	}
	return false, nil
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

	// Some kind of a canary to ensure that the CRDs are actually installed and established
	// (i.e. their REST API is being served) before proceeding. Merely checking that the CRD
	// object exists is not enough: there's a window between CRD creation and the API server
	// starting to serve its resources, during which RESTMapper lookups fail with
	// "no matches for kind".
	// ponytail: retry.DefaultRetry (~50ms total) is only enough to wait for object
	// existence - apply is synchronous. Established is set asynchronously by the
	// naming controller, which needs a real budget under CI load.
	crdEstablishedBackoff := wait.Backoff{
		Duration: 200 * time.Millisecond,
		Factor:   1.5,
		Steps:    30,
		Cap:      5 * time.Second,
	}
	for _, crd := range []string{
		"gateways.gateway.networking.k8s.io",                         // GWAPI CRD
		"dataplanes.gateway-operator.konghq.com",                     // kong-operator
		"kongservicefacades.incubator.ingress-controller.konghq.com", // ingress-controller-incubator
	} {
		if err := retry.OnError(
			crdEstablishedBackoff,
			func(err error) bool {
				return apierrors.IsNotFound(err) || errors.Is(err, errCRDNotEstablished)
			},
			func() error {
				established, err := isCRDEstablished(ctx, apiextClient, crd)
				if err != nil {
					return err
				}
				if !established {
					return fmt.Errorf("%w: %s", errCRDNotEstablished, crd)
				}
				return nil
			},
		); err != nil {
			return err
		}
	}

	return nil
}
