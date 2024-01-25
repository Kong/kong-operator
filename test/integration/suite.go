//go:build integration_tests || integration_tests_bluegreen

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/config"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/modules/admission"
	"github.com/kong/gateway-operator/modules/manager"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
	runWebhookTests      = false
	webhookCertDir       = ""
	webhookServerIP      = os.Getenv("GATEWAY_OPERATOR_WEBHOOK_IP")
	bluegreenController  = strings.ToLower(os.Getenv("GATEWAY_OPERATOR_BLUEGREEN_CONTROLLER")) == "true"
	webhookServerPort    = 9443
	disableCalicoCNI     = strings.ToLower(os.Getenv("KONG_TEST_DISABLE_CALICO")) == "true"
)

// -----------------------------------------------------------------------------
// Testing Vars - Testing Environment
// -----------------------------------------------------------------------------

var testSuite []func(*testing.T)

// GetTestSuite returns all integration tests that should be run.
func GetTestSuite() []func(*testing.T) {
	return testSuite
}

func addTestsToTestSuite(tests ...func(*testing.T)) {
	testSuite = append(testSuite, tests...)
}

var (
	ctx     context.Context
	env     environments.Environment
	clients testutils.K8sClients

	httpc = http.Client{
		Timeout: time.Second * 10,
	}
)

// GetCtx returns the context used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain.
func GetCtx() context.Context {
	return ctx
}

// GetEnv returns the environment used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain
func GetEnv() environments.Environment {
	return env
}

// GetClients returns the Kubernetes clients used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain
func GetClients() testutils.K8sClients {
	return clients
}

// RunTestSuite runs all tests from the test suite.
// Import and call in a test function. Environment needs to be bootstrapped
// in TestMain.
func RunTestSuite(t *testing.T, testSuite []func(*testing.T)) {
	duplicates := lo.FindDuplicatesBy(testSuite, func(f func(*testing.T)) string {
		return getFunctionName(f)
	})
	duplicatesNames := lo.Map(duplicates, func(f func(*testing.T), _ int) string {
		return getFunctionName(f)
	})
	require.Empty(t, duplicatesNames, "duplicate test functions found in test suite")
	t.Log("INFO: running test suite")
	for _, test := range testSuite {
		t.Run(getFunctionName(test), test)
	}
}

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

// TestMain is the entrypoint for the integration test suite. It bootstraps
// the testing environment and runs the test suite. Call it from TestMain.
func TestMain(m *testing.M) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	closeControllerLogFile, err := testutils.SetupControllerLogger(controllerManagerOut)
	exitOnErr(err)
	defer closeControllerLogFile() //nolint:errcheck

	fmt.Println("INFO: configuring cluster for testing environment")
	env, err = testutils.BuildEnvironment(ctx, existingCluster,
		func(b *environments.Builder) {
			if !disableCalicoCNI {
				b.WithCalicoCNI()
			}
		},
	)
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", env.Cluster().Name())
	exitOnErr(<-env.WaitForReady(ctx))

	fmt.Println("INFO: initializing Kubernetes clients")
	clients, err = testutils.NewK8sClients(env)
	exitOnErr(err)

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(ctx, env.Cluster(), "kong-system"))

	configPath, cleaner, err := config.DumpKustomizeConfigToTempDir()
	exitOnErr(err)
	defer cleaner()

	exitOnErr(clusters.KustomizeDeployForCluster(ctx, env.Cluster(), path.Join(configPath, "/rbac")))

	// normally this is obtained from the downward API. the tests fake it.
	err = os.Setenv("POD_NAMESPACE", "kong-system")
	exitOnErr(err)

	fmt.Println("INFO: deploying CRDs to test cluster")
	exitOnErr(testutils.DeployCRDs(ctx, path.Join(configPath, "/crd"), clients.OperatorClient, env))

	runWebhookTests = (os.Getenv("RUN_WEBHOOK_TESTS") == "true")
	if runWebhookTests {
		exitOnErr(prepareWebhook())
	}

	fmt.Println("INFO: starting the operator's controller manager")
	// startControllerManager will spawn the controller manager in a separate
	// goroutine and will report whether that succeeded.
	started := startControllerManager()
	<-started

	exitOnErr(testutils.BuildMTLSCredentials(ctx, clients.K8sClient, &httpc))

	// wait for webhook server in controller to be ready after controller started.
	if runWebhookTests {
		exitOnErr(waitForWebhook(ctx, webhookServerIP, webhookServerPort))
	}

	fmt.Println("INFO: environment is ready, starting tests")
	code := m.Run()

	if !skipClusterCleanup && existingCluster == "" {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(env.Cleanup(ctx))
	}

	os.Exit(code)
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if !skipClusterCleanup && existingCluster == "" {
			if env != nil {
				env.Cleanup(ctx) //nolint:errcheck
			}
		}
		panic(fmt.Sprintf("ERROR: %s\n", err.Error()))
	}
}

// startControllerManager will configure the manager and start it in a separate goroutine.
// It returns a channel which will get closed when manager.Start() gets called.
func startControllerManager() <-chan struct{} {
	cfg := manager.DefaultConfig()
	cfg.LeaderElection = false
	cfg.DevelopmentMode = true
	cfg.ControllerName = "konghq.com/gateway-operator-integration-tests"
	cfg.GatewayControllerEnabled = true
	cfg.ControlPlaneControllerEnabled = true
	cfg.DataPlaneControllerEnabled = true
	cfg.DataPlaneBlueGreenControllerEnabled = bluegreenController
	cfg.ValidatingWebhookEnabled = false
	cfg.AnonymousReports = false
	cfg.StartedCh = make(chan struct{})

	if runWebhookTests {
		cfg.WebhookCertDir = webhookCertDir
	}

	cfg.NewClientFunc = func(config *rest.Config, options client.Options) (client.Client, error) {
		// always hijack and impersonate the system service account here so that the manager
		// is testing the RBAC permissions we provide under config/rbac/. This helps alert us
		// if we break our RBAC configs as the manager will emit permissions errors.
		config.Impersonate.UserName = "system:serviceaccount:kong-system:controller-manager"

		return client.New(config, options)
	}

	go func() {
		exitOnErr(manager.Run(cfg, manager.SetupControllers, admission.NewRequestHandler))
	}()

	return cfg.StartedCh
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
		ctx, clients.K8sClient,
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
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	for !ready {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// any kind of response from /validate path is considered OK
			resp, err := client.Get(fmt.Sprintf("https://%s:%d/validate", ip, port))
			if err == nil {
				_ = resp.Body.Close()
				ready = true
			}
		}
		if !ready {
			time.Sleep(time.Second)
		}
	}
	return nil
}
