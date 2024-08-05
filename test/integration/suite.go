package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/config"
	"github.com/kong/gateway-operator/modules/manager"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/helpers/certificate"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
	webhookEnabled       = strings.ToLower(os.Getenv("WEBHOOK_ENABLED")) == "true"
	webhookServerIP      = os.Getenv("GATEWAY_OPERATOR_WEBHOOK_IP")
	bluegreenController  = strings.ToLower(os.Getenv("GATEWAY_OPERATOR_BLUEGREEN_CONTROLLER")) == "true"
	webhookServerPort    = 9443
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

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

// SetUpAndRunManagerFunc is the type of the callback that is passed to TestMain.
// This id called to set up and run the controller manager. Returned error should be
// a result of calling manager.Run.
type SetUpAndRunManagerFunc func(startedChan chan struct{}) error

// TestMain is the entrypoint for the integration test suite. It bootstraps
// the testing environment and runs the test suite on instance of KGO
// constructed by the argument setUpAndRunManager. This callback is called,
// when the whole cluster is ready and the controller manager can be started.
// Thus it can be used e.g. to apply additional resources to the cluster too.
func TestMain(
	m *testing.M,
	setUpAndRunManager SetUpAndRunManagerFunc,
) {
	var code int
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v stack trace:\n%s\n", r, debug.Stack())
			code = 1
		}
		os.Exit(code)
	}()
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	closeControllerLogFile, err := testutils.SetupControllerLogger(controllerManagerOut)
	exitOnErr(err)
	defer closeControllerLogFile() //nolint:errcheck

	fmt.Println("INFO: configuring cluster for testing environment")
	env, err = testutils.BuildEnvironment(GetCtx(), existingCluster,
		func(b *environments.Builder, ct clusters.Type) {
			if !test.IsCalicoCNIDisabled() {
				b.WithCalicoCNI()
			}
			if !test.IsCertManagerDisabled() {
				b.WithAddons(certmanager.New())
			}
			if !test.IsMetalLBDisabled() && ct == kind.KindClusterType {
				b.WithAddons(metallb.New())
			}
		},
	)
	exitOnErr(err)

	fmt.Printf("INFO: waiting for cluster %s and all addons to become ready\n", GetEnv().Cluster().Name())
	exitOnErr(<-GetEnv().WaitForReady(GetCtx()))

	fmt.Println("INFO: initializing Kubernetes clients")
	clients, err = testutils.NewK8sClients(GetEnv())
	exitOnErr(err)

	fmt.Println("INFO: creating system namespaces and serviceaccounts")
	exitOnErr(clusters.CreateNamespace(GetCtx(), GetEnv().Cluster(), "kong-system"))

	configPath, cleaner, err := config.DumpKustomizeConfigToTempDir()
	exitOnErr(err)
	defer cleaner()

	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/base")))
	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/role")))

	// normally this is obtained from the downward API. the tests fake it.
	err = os.Setenv("POD_NAMESPACE", "kong-system")
	exitOnErr(err)

	fmt.Println("INFO: deploying CRDs to test cluster")
	exitOnErr(testutils.DeployCRDs(GetCtx(), path.Join(configPath, "/crd"), GetClients().OperatorClient, GetEnv()))

	var ca, cert, key []byte // Certificate generated for tests used by webhook.
	if webhookEnabled {
		ca, cert, key, err = prepareWebhook()
		exitOnErr(err)
	}

	fmt.Println("INFO: starting the operator's controller manager")
	// Spawn the controller manager based on passed config in
	// a separate goroutine and report whether that succeeded.
	startedChan := make(chan struct{})
	go func() {
		exitOnErr(setUpAndRunManager(startedChan))
	}()
	<-startedChan

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	exitOnErr(err)
	exitOnErr(testutils.BuildMTLSCredentials(GetCtx(), GetClients().K8sClient, httpClient))

	// Wait for webhook server in controller to be ready after controller started.
	if webhookEnabled {
		exitOnErr(waitForWebhook(GetCtx(), webhookServerIP, webhookServerPort, ca, cert, key))
	}

	fmt.Println("INFO: environment is ready, starting tests")
	code = m.Run()

	if !skipClusterCleanup && existingCluster == "" {
		fmt.Println("INFO: cleaning up testing cluster and environment")
		exitOnErr(GetEnv().Cleanup(GetCtx()))
	}
}

// -----------------------------------------------------------------------------
// Testing Main - Helper Functions
// -----------------------------------------------------------------------------

func exitOnErr(err error) {
	if err != nil {
		if !skipClusterCleanup && existingCluster == "" {
			if GetEnv() != nil {
				GetEnv().Cleanup(GetCtx()) //nolint:errcheck
			}
		}
		panic(fmt.Sprintf("ERROR: %s\n", err.Error()))
	}
}

// DefaultControllerConfigForTests returns a default configuration for the controller manager used in tests.
// It can be adjusted by overriding arbitrary fields in the returned config.
func DefaultControllerConfigForTests() manager.Config {
	cfg := manager.DefaultConfig()
	cfg.LeaderElection = false
	cfg.DevelopmentMode = true
	cfg.ControllerName = "konghq.com/gateway-operator-integration-tests"
	cfg.GatewayControllerEnabled = true
	cfg.ControlPlaneControllerEnabled = true
	cfg.DataPlaneControllerEnabled = true
	cfg.DataPlaneBlueGreenControllerEnabled = bluegreenController
	cfg.KongPluginInstallationControllerEnabled = true
	cfg.AIGatewayControllerEnabled = true
	cfg.ValidatingWebhookEnabled = webhookEnabled
	cfg.AnonymousReports = false

	cfg.NewClientFunc = func(config *rest.Config, options client.Options) (client.Client, error) {
		// always hijack and impersonate the system service account here so that the manager
		// is testing the RBAC permissions we provide under config/rbac/. This helps alert us
		// if we break our RBAC configs as the manager will emit permissions errors.
		config.Impersonate.UserName = "system:serviceaccount:kong-system:controller-manager"

		return client.New(config, options)
	}
	return cfg
}

// prepareWebhook prepares for running webhook if we are going to run webhook tests. includes:
// - creating self-signed TLS certificates for webhook server
// - creating validating webhook resource in test cluster.
func prepareWebhook() (ca, cert, key []byte, err error) {
	// Get IP for generating certificate and for clients to access.
	if webhookServerIP == "" {
		webhookServerIP = helpers.GetAdmissionWebhookListenHost()
	}
	// Generate certificates for webhooks.
	// It must run before we start controller manager to start webhook server in controller.
	cert, key = certificate.MustGenerateSelfSignedCertPEMFormat(certificate.WithIPAdresses(webhookServerIP))
	ca = cert // Self-signed certificate is its own CA.

	// Create webhook resources in k8s.
	fmt.Println("INFO: creating a validating webhook and waiting for it to start")
	if err = CreateValidatingWebhook(
		GetCtx(), GetClients().K8sClient,
		fmt.Sprintf("https://%s:%d/validate", webhookServerIP, webhookServerPort),
		ca, cert, key,
	); err != nil {
		return nil, nil, nil, err
	}
	return ca, cert, key, nil
}

// waitForWebhook waits for webhook server being able to be accessed by HTTPS.
func waitForWebhook(ctx context.Context, ip string, port int, ca, cert, key []byte) error {
	certFull, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return err
	}
	// Setup HTTPS client.
	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(ca); !ok {
		return fmt.Errorf("failed to append CA certificate to pool")
	}
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				Certificates: []tls.Certificate{certFull},
				RootCAs:      caCertPool,
			},
		},
	}
	for ready := false; !ready; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Any kind of response from /validate path is considered OK.
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
