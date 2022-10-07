package test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kong/gateway-operator/internal/manager"
	operatorclient "github.com/kong/gateway-operator/pkg/clientset"
)

func noOpClose() error {
	return nil
}

// SetupControllerLogger sets up the controller logger.
// This functions needs to be called before 30sec after the controller packages
// is loaded, otherwise the logger will not be initialized.
// Args:
//   - controllerManagerOut: the path to the file where the controller logs should be written to or "stdout".
//
// Returns:
//   - The close function, that will close the log file if one was created. Should be called  after the tests are done.
//   - An error on failure.
func SetupControllerLogger(controllerManagerOut string) (func() error, error) {
	var destFile *os.File
	var destWriter io.Writer = os.Stdout

	if controllerManagerOut != "stdout" {
		out, err := os.CreateTemp("", "gateway-operator-controller-logs")
		if err != nil {
			return noOpClose, err
		}
		fmt.Printf("INFO: controller output is being logged to %s\n", out.Name())
		destWriter = out
		destFile = out
	}

	opts := zap.Options{
		Development: true,
		DestWriter:  destWriter,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	closeLogFile := func() error {
		if destFile != nil {
			return destFile.Close()
		}
		return nil
	}

	return closeLogFile, nil
}

// WaitForOperatorCRDs waits for the operator CRDs to be available.
func WaitForOperatorCRDs(ctx context.Context, operatorClient *operatorclient.Clientset) error {
	ready := false
	for !ready {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			_, err := operatorClient.ApisV1alpha1().DataPlanes(corev1.NamespaceDefault).List(ctx, metav1.ListOptions{})
			if err == nil {
				ready = true
			}
		}
	}
	return nil
}

// BuildEnvironment builds the k8s environment for the tests.
// Args:
//   - ctx: the context to use for the environment build.
//   - existingCluster: the name of the existing cluster to use for the tests. If empty, a new kind cluster will be created.
//
// Returns the environment on success and an error on failure.
func BuildEnvironment(ctx context.Context, existingCluster string) (environments.Environment, error) {
	if existingCluster != "" {
		fmt.Println("INFO: existing cluster found, deploying on existing cluster")
		return buildEnvironmentOnExistingCluster(ctx, existingCluster)
	}

	fmt.Println("INFO: no existing cluster found, deploying using Kubernetes In Docker (KIND)")
	return buildEnvironmentOnNewKindCluster(ctx)
}

func buildEnvironmentOnNewKindCluster(ctx context.Context) (environments.Environment, error) {
	builder := environments.NewBuilder()
	builder.WithCalicoCNI()
	builder.WithAddons(metallb.New())
	return builder.Build(ctx)
}

func buildEnvironmentOnExistingCluster(ctx context.Context, existingCluster string) (environments.Environment, error) {
	builder := environments.NewBuilder()

	clusterParts := strings.Split(existingCluster, ":")
	if len(clusterParts) != 2 {
		return nil, fmt.Errorf("existing cluster in wrong format (%s): format is <TYPE>:<NAME> (e.g. kind:test-cluster)", existingCluster)
	}
	clusterType, clusterName := clusterParts[0], clusterParts[1]

	fmt.Printf("INFO: using existing %s cluster %s\n", clusterType, clusterName)
	switch clusterType {
	case string(kind.KindClusterType):
		cluster, err := kind.NewFromExisting(clusterName)
		if err != nil {
			return nil, err
		}
		builder.WithExistingCluster(cluster)
		builder.WithAddons(metallb.New())
	case string(gke.GKEClusterType):
		cluster, err := gke.NewFromExistingWithEnv(ctx, clusterName)
		if err != nil {
			return nil, err
		}
		builder.WithExistingCluster(cluster)
	default:
		return nil, fmt.Errorf("unknown cluster type: %s", clusterType)
	}

	return builder.Build(ctx)
}

// BuildMTLSCredentials builds the mTLS credentials for the tests.
// Args:
//   - ctx: the context to use.
//   - k8sClient: the k8s client to use.
//   - httpc: the http client to configure with the mTLS credentials.
func BuildMTLSCredentials(ctx context.Context, k8sClient *kubernetes.Clientset, httpc *http.Client) error {
	timeout := time.Now().Add(time.Minute)
	for timeout.After(time.Now()) {
		err := func() error {
			ca, err := k8sClient.CoreV1().Secrets("kong-system").Get(ctx, manager.DefaultConfig().ClusterCASecretName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cert, err := tls.X509KeyPair(ca.Data["tls.crt"], ca.Data["tls.key"])
			if err != nil {
				return err
			}

			transport := &http.Transport{
				TLSClientConfig: &tls.Config{
					Certificates:       []tls.Certificate{cert},
					InsecureSkipVerify: true, //nolint:gosec
				},
			}
			httpc.Transport = transport
			return nil
		}()
		if err != nil {
			time.Sleep(time.Second)
		}
	}
	return nil
}

// DeployCRDs deploys the CRDs commonly used in tests.
func DeployCRDs(ctx context.Context, operatorClient *operatorclient.Clientset, env environments.Environment) error {
	if err := clusters.KustomizeDeployForCluster(ctx, env.Cluster(), "../../config/crd"); err != nil {
		return err
	}
	if err := clusters.KustomizeDeployForCluster(ctx, env.Cluster(), GatewayCRDsKustomizeURL); err != nil {
		return err
	}
	if err := WaitForOperatorCRDs(ctx, operatorClient); err != nil {
		return err
	}
	return nil
}
