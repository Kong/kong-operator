package integration

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager"
	"github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
	"github.com/kong/kong-operator/v2/test/helpers/webhook"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
	// BlueGreenControllerEnabled indicates whether the blue-green controller
	// should be enabled in the controller manager for tests in this suite.
	BlueGreenControllerEnabled = strings.ToLower(os.Getenv("KONG_OPERATOR_BLUEGREEN_CONTROLLER")) == "true"
)

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
// by TestMain.
func GetEnv() environments.Environment {
	return env
}

// GetClients returns the Kubernetes clients used by the test suite.
// It allows interaction in tests with environment bootstrapped
// by TestMain.
func GetClients() testutils.K8sClients {
	return clients
}

// SuiteOption configures optional behavior of Suite.
type SuiteOption func(*suiteOptions)

type suiteOptions struct {
	createKongLicense bool
}

// WithKongLicense makes Suite present a Kong Enterprise license (read from
// the KONG_LICENSE_DATA environment variable) to the test cluster by creating
// a KongLicense resource, the same way test/conformance and
// test/integration/kic/isolated do. It's a no-op if KONG_LICENSE_DATA is not set.
func WithKongLicense() SuiteOption {
	return func(o *suiteOptions) {
		o.createKongLicense = true
	}
}

// Suite sets up the testing environment for the integration test suite.
// It is intended to be called from TestMain in the respective test package.
func Suite(m *testing.M, opts ...SuiteOption) {
	var so suiteOptions
	for _, opt := range opts {
		opt(&so)
	}

	var code int
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%v stack trace:\n%s\n", r, debug.Stack())
			code = 1
		}
		os.Exit(code)
	}()

	helpers.SetDefaultDataPlaneImage(consts.DefaultDataPlaneImage)
	helpers.SetDefaultDataPlaneBaseImage(consts.DefaultDataPlaneBaseImage)

	cfg := testutils.DefaultControllerConfigForTests(testutils.WithBlueGreenController(BlueGreenControllerEnabled))
	controllerNamespace := cfg.ControllerNamespace

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

	cleanupControllerResources, err := helpers.SetupControllerOperatorResources(GetCtx(), controllerNamespace, clients.MgrClient)
	exitOnErr(err)
	defer cleanupControllerResources()

	fmt.Println("INFO: Deploying all required Kubernetes Configuration (RBAC, CRDs, etc.) for the operator")
	exitOnErr(kcfg.DeployKubernetesConfiguration(GetCtx(), env.Cluster()))

	if so.createKongLicense {
		cleanupKongLicense, err := createKongLicense(GetCtx(), clients.MgrClient)
		exitOnErr(err)
		defer cleanupKongLicense()
	}

	cleanupTelepresence, err := helpers.SetupTelepresence(ctx)
	exitOnErr(err)
	defer cleanupTelepresence()

	// Setup environment for NetworkPolicy testing.
	// See: https://github.com/Kong/kong-operator/issues/2074
	helpers.SetupKubernetesServiceHost(GetEnv().Cluster().Config())
	cleanupPodLabels, err := helpers.SetupFakePodLabels()
	exitOnErr(err)
	defer cleanupPodLabels()

	fmt.Println("INFO: configuring validating webhook")
	checkConnectivityToWebhook, cleanupWebhook, err := webhook.EnsureValidatingWebhookRegistration(
		GetCtx(), clients.K8sClient, controllerNamespace,
	)
	exitOnErr(err)
	defer cleanupWebhook()

	fmt.Println("INFO: starting the operator's controller manager")
	// Spawn the controller manager based on passed config in
	// a separate goroutine and report whether that succeeded.
	managerToTest := func(startedChan chan struct{}) error {
		return manager.Run(cfg, scheme.Get(), manager.SetupControllers, startedChan, metadata.Metadata())
	}
	startedChan := make(chan struct{})
	go func() {
		exitOnErr(managerToTest(startedChan))
	}()
	<-startedChan

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	exitOnErr(err)
	exitOnErr(testutils.BuildMTLSCredentials(GetCtx(), GetClients().K8sClient, httpClient))

	fmt.Println("INFO: waiting for webhook Service to be connective")
	exitOnErr(checkConnectivityToWebhook())

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

// createKongLicense creates a KongLicense resource read from the
// KONG_LICENSE_DATA environment variable, so that any ControlPlane started
// during the suite's tests can pick it up. Returns a no-op cleanup and no
// error if KONG_LICENSE_DATA is not set.
func createKongLicense(ctx context.Context, cl ctrlruntimeclient.Client) (func(), error) {
	licenseData := test.KongLicenseData()
	if licenseData == "" {
		fmt.Println("INFO: KONG_LICENSE_DATA not set, skipping KongLicense creation")
		return func() {}, nil
	}

	fmt.Println("INFO: creating KongLicense for tests")
	kongLicense := &configurationv1alpha1.KongLicense{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ko-integration-license-",
		},
		RawLicenseString: licenseData,
		Enabled:          true,
	}
	if err := cl.Create(ctx, kongLicense); err != nil {
		return nil, fmt.Errorf("failed to create KongLicense: %w", err)
	}

	return func() {
		if err := cl.Delete(context.Background(), kongLicense); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("WARN: failed to delete KongLicense %s: %s\n", kongLicense.Name, err)
		}
	}, nil
}

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
