//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/manager"
	"github.com/kong/gateway-operator/pkg/clientset"
)

// Testing Consts
// -----------------------------------------------------------------------------

const gatewayAPIsCRDs = "https://github.com/kubernetes-sigs/gateway-api.git/config/crd?ref=bcfe3da78648b4206a627fe7f3b2d0db7e755ba8"

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingClusterName  = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   bool
	runWebhookTests      = false
	webhookCertDir       = ""
	webhookServerIP      = os.Getenv("GATEWAY_OPERATOR_WEBHOOK_IP")
	webhookServerPort    = 9443
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
	gatewayClient  *gatewayclient.Clientset
	mgrClient      client.Client

	httpc = http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	}
)

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	closeControllerLogFile := setupControllerLogger()
	defer closeControllerLogFile()

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
	env, err = envBuilder.WithAddons(metallb.New()).Build(ctx)
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	exitOnErr(<-env.WaitForReady(ctx))

	fmt.Println("INFO: initializing Kubernetes API clients")
	k8sClient = env.Cluster().Client()
	operatorClient, err = clientset.NewForConfig(env.Cluster().Config())
	exitOnErr(err)
	gatewayClient, err = gatewayclient.NewForConfig(env.Cluster().Config())
	exitOnErr(err)

	fmt.Println("INFO: intializing manager client")
	mgrClient, err = client.New(env.Cluster().Config(), client.Options{})
	exitOnErr(err)
	exitOnErr(gatewayv1alpha2.AddToScheme(mgrClient.Scheme()))
	exitOnErr(operatorv1alpha1.AddToScheme(mgrClient.Scheme()))

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), "../../config/rbac"))

	fmt.Println("INFO: deploying CRDs to test cluster")
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), "../../config/crd"))
	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), gatewayAPIsCRDs))
	exitOnErr(waitForCRDs(ctx))

	runWebhookTests = (os.Getenv("RUN_WEBHOOK_TESTS") == "true")
	if runWebhookTests {
		exitOnErr(prepareWebhook())
	}

	fmt.Println("INFO: starting the operator's controller manager")
	go startControllerManager()

	// wait for webhook server in controller to be ready after controller started.
	if runWebhookTests {
		exitOnErr(waitForWebhook(ctx, webhookServerIP, webhookServerPort))
	}

	fmt.Println("INFO: environment is ready, starting tests")
	code := m.Run()

	if !skipClusterCleanup {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(env.Cleanup(ctx))
	}

	os.Exit(code)
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if !skipClusterCleanup && err != nil {
		if env != nil {
			env.Cleanup(ctx) //nolint:errcheck
		}
		fmt.Printf("ERROR: %s\n", err.Error())
		os.Exit(1)
	}
}

// setupControllerLogger sets up the controller logger.
// This functions needs to be called before 30sec after the controller packages
// is loaded, otherwise the logger will not be initialized.
// Returns the close function, that will close the log file if one was created.
func setupControllerLogger() (closeLogFile func() error) {
	var destFile *os.File
	var destWriter io.Writer = os.Stdout

	if controllerManagerOut != "stdout" {
		out, err := os.CreateTemp("", "gateway-operator-controller-logs")
		exitOnErr(err)
		fmt.Printf("INFO: controller output is being logged to %s\n", out.Name())
		destWriter = out
		destFile = out
	}

	opts := zap.Options{
		Development: true,
		DestWriter:  destWriter,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	closeLogFile = func() error {
		if destFile != nil {
			return destFile.Close()
		}
		return nil
	}

	return closeLogFile
}

func startControllerManager() {
	cfg := manager.DefaultConfig
	cfg.LeaderElection = false
	cfg.DevelopmentMode = true
	cfg.ControllerName = "konghq.com/gateway-operator-integration-tests"

	if runWebhookTests {
		cfg.WebhookCertDir = webhookCertDir
	}

	cfg.NewClientFunc = func(cache cache.Cache, config *rest.Config, options client.Options, uncachedObjects ...client.Object) (client.Client, error) {
		// always hijack and impersonate the system service account here so that the manager
		// is testing the RBAC permissions we provide under config/rbac/. This helps alert us
		// if we break our RBAC configs as the manager will emit permissions errors.
		config.Impersonate.UserName = "system:serviceaccount:kong-system:controller-manager"

		c, err := client.New(config, options)
		if err != nil {
			return nil, err
		}

		return client.NewDelegatingClient(client.NewDelegatingClientInput{
			CacheReader:     cache,
			Client:          c,
			UncachedObjects: uncachedObjects,
		})
	}

	if controllerManagerOut != "stdout" {
		out, err := os.CreateTemp(os.TempDir(), "gateway-operator-controller-logs")
		exitOnErr(err)
		cfg.Out = out
		fmt.Printf("INFO: controller output is being logged to %s\n", out.Name())
		defer out.Close()
	}

	exitOnErr(manager.Run(cfg))
}

func waitForCRDs(ctx context.Context) error {
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

func generateWebhookCertificates() error {
	// generate certificates for webhook.
	fmt.Println("INFO: creating certificates for running webhook tests")
	dir, err := os.MkdirTemp(os.TempDir(), "gateway-operator-webhook-certs")
	if err != nil {
		return err
	}
	webhookCertDir = dir

	fmt.Println("INFO: creating certificates in directory", webhookCertDir)
	cmd := exec.CommandContext(ctx, "../../hack/generate-certificates-openssl.sh", webhookCertDir, webhookServerIP)
	return cmd.Run()
}

// prepareWebhook prepares for running webhook if we are going to run webhook tests. includes:
// - creating self-signed TLS certificates for webhook server
// - creating validaing webhook resource in test cluster
func prepareWebhook() error {
	// get IP for generating certificate and for clients to access.
	if webhookServerIP == "" {
		var getIPErr error
		webhookServerIP, getIPErr = getFirstNonLoopbackIP()
		if getIPErr != nil {
			return getIPErr
		}
	}

	// generate certificates for webhooks.
	// must run before we start controller manager to start webhook server in controller.
	err := generateWebhookCertificates()
	if err != nil {
		return err
	}

	// create webhook resources in k8s.
	fmt.Println("INFO: creating a validating webhook and waiting for it to start")
	return createValidatingWebhook(
		ctx, k8sClient,
		fmt.Sprintf("https://%s:%d/validate", webhookServerIP, webhookServerPort),
		webhookCertDir+"/ca.crt",
	)
}

// waitForWebhook waits for webhook server being able to be accessed by HTTPS.
func waitForWebhook(ctx context.Context, ip string, port int) error {
	ready := false

	certFile := webhookCertDir + "/tls.crt"
	keyFile := webhookCertDir + "/tls.key"
	caFile := webhookCertDir + "/ca.crt"

	// Load client cert
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}

	// Load CA cert
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	for !ready {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// any kind of response from /validate path is considered OK
			_, err := client.Get(fmt.Sprintf("https://%s:%d/validate", ip, port))
			if err == nil {
				ready = true
			}
		}
		if !ready {
			time.Sleep(time.Second)
		}
	}
	return nil
}
