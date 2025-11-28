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
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kong/kong-operator/controller/pkg/secrets"
	"github.com/kong/kong-operator/modules/manager"
)

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
			// noOpClose
			return func() error {
				return nil
			}, err
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
	secrets.SetCALogger(ctrl.Log)

	closeLogFile := func() error {
		if destFile != nil {
			return destFile.Close()
		}
		return nil
	}

	return closeLogFile, nil
}

// BuilderOpt is an option function for an environment builder.
type BuilderOpt func(*environments.Builder, clusters.Type)

// BuildEnvironment builds the k8s environment for the tests.
// Args:
//   - ctx: the context to use for the environment build.
//   - existingCluster: the name of the existing cluster to use for the tests. If empty, a new kind cluster will be created.
//   - builderOpts: accept a list of builder options that will be applied to the builder before buildling the environment.
//
// Returns the environment on success and an error on failure.
func BuildEnvironment(ctx context.Context, existingCluster string, builderOpts ...BuilderOpt) (environments.Environment, error) {
	if existingCluster != "" {
		fmt.Println("INFO: existing cluster found, deploying on existing cluster")
		return buildEnvironmentOnExistingCluster(ctx, existingCluster, builderOpts...)
	}

	fmt.Println("INFO: no existing cluster found, deploying using Kubernetes In Docker (KIND)")
	return buildEnvironmentOnNewKindCluster(ctx, builderOpts...)
}

func buildEnvironmentOnNewKindCluster(ctx context.Context, builderOpts ...BuilderOpt) (environments.Environment, error) {
	builder := environments.NewBuilder()

	for _, o := range builderOpts {
		o(builder, kind.KindClusterType)
	}
	return builder.Build(ctx)
}

func buildEnvironmentOnExistingCluster(ctx context.Context, existingCluster string, builderOpts ...BuilderOpt) (environments.Environment, error) {
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
	case string(gke.GKEClusterType):
		cluster, err := gke.NewFromExistingWithEnv(ctx, clusterName)
		if err != nil {
			return nil, err
		}
		builder.WithExistingCluster(cluster)
	default:
		return nil, fmt.Errorf("unknown cluster type: %s", clusterType)
	}

	for _, o := range builderOpts {
		o(builder, clusters.Type(clusterType))
	}

	return builder.Build(ctx)
}

// BuildMTLSCredentials builds the mTLS credentials for the tests.
// Args:
//   - ctx: the context to use.
//   - k8sClient: the k8s client to use.
//   - httpc: the http client to configure with the mTLS credentials.
func BuildMTLSCredentials(ctx context.Context, k8sClient *kubernetes.Clientset, httpc *http.Client) error {
	var (
		err     error
		timeout = time.After(time.Minute)
		ticker  = time.NewTicker(time.Second)
	)

	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to BuildMTLSCredentials: %w", ctx.Err())

		case <-timeout:
			return fmt.Errorf("failed to BuildMTLSCredentials: %w", err)

		case <-ticker.C:
			ca, localErr := k8sClient.CoreV1().Secrets("kong-system").Get(ctx,
				manager.DefaultConfig().ClusterCASecretName, metav1.GetOptions{},
			)
			if localErr != nil {
				err = localErr
				continue
			}

			cert, localErr := tls.X509KeyPair(ca.Data["tls.crt"], ca.Data["tls.key"])
			if localErr != nil {
				err = localErr
				continue
			}

			transport := &http.Transport{
				TLSClientConfig: &tls.Config{
					Certificates:       []tls.Certificate{cert},
					InsecureSkipVerify: true, //nolint:gosec
				},
			}
			httpc.Transport = transport
			return nil
		}
	}
}
