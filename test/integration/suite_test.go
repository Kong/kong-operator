package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/certmanager"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/metallb"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/kind"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"

	"github.com/kong/kong-operator/modules/manager"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
)

// -----------------------------------------------------------------------------
// Testing Vars - Environment Overrideable
// -----------------------------------------------------------------------------

var (
	existingCluster      = os.Getenv("KONG_TEST_CLUSTER")
	controllerManagerOut = os.Getenv("KONG_CONTROLLER_OUT")
	skipClusterCleanup   = strings.ToLower(os.Getenv("KONG_TEST_CLUSTER_PERSIST")) == "true"
	blueGreenController  = strings.ToLower(os.Getenv("KONG_OPERATOR_BLUEGREEN_CONTROLLER")) == "true"
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

func TestMain(m *testing.M) {
	helpers.SetDefaultDataPlaneImage(consts.DefaultDataPlaneImage)
	helpers.SetDefaultDataPlaneBaseImage(consts.DefaultDataPlaneBaseImage)

	cfg := testutils.DefaultControllerConfigForTests(testutils.WithBlueGreenController(blueGreenController))

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

	configPath := path.Join(testutils.ProjectRootPath(), "config")

	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/base")))
	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/rbac/role")))
	exitOnErr(clusters.KustomizeDeployForCluster(GetCtx(), GetEnv().Cluster(), path.Join(configPath, "/default/validating_policies")))

	// normally this is obtained from the downward API. the tests fake it.
	err = os.Setenv("POD_NAMESPACE", "kong-system")
	exitOnErr(err)

	if !test.IsInstallingCRDsDisabled() {
		fmt.Println("INFO: deploying CRDs to test cluster")
		exitOnErr(testutils.DeployCRDs(GetCtx(), path.Join(configPath, "/crd"), GetClients().OperatorClient, GetEnv().Cluster()))
	}

	cleanupTelepresence, err := helpers.SetupTelepresence(ctx)
	exitOnErr(err)
	defer cleanupTelepresence()

	// Set KUBERNETES_SERVICE_HOST to enable NetworkPolicy creation in tests.
	// When running with telepresence, the controller can access cluster services
	// and should create NetworkPolicies just like it would when deployed in-cluster.
	// See: https://github.com/Kong/kong-operator/issues/2074
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		// Get the Kubernetes API server host from the cluster config
		restConfig := GetEnv().Cluster().Config()
		if restConfig.Host != "" {
			apiURL, err := url.Parse(restConfig.Host)
			if err == nil && apiURL.Host != "" {
				fmt.Printf("INFO: setting KUBERNETES_SERVICE_HOST=%s for NetworkPolicy support\n", apiURL.Hostname())
				os.Setenv("KUBERNETES_SERVICE_HOST", apiURL.Hostname())
			}
		}
	}

	// Create fake pod labels file for testing.
	// The controller expects this file when RunningOnKubernetes() is true
	// (i.e., when KUBERNETES_SERVICE_HOST is set).
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		podLabelsDir := "/etc/podinfo"
		podLabelsFile := path.Join(podLabelsDir, "labels")
		if _, err := os.Stat(podLabelsFile); os.IsNotExist(err) {
			fmt.Printf("INFO: creating pod labels file at %s\n", podLabelsFile)
			// Try to create the directory. If permission is denied, try with sudo.
			if err := os.MkdirAll(podLabelsDir, 0755); err != nil {
				// Permission denied, try with sudo
				fmt.Printf("INFO: permission denied, retrying with sudo\n")
				cmd := exec.Command("sudo", "mkdir", "-p", podLabelsDir)
				if output, err := cmd.CombinedOutput(); err != nil {
					exitOnErr(fmt.Errorf("failed to create directory %s with sudo: %w, output: %s", podLabelsDir, err, string(output)))
				}
			}

			// Write the file content to a temporary location first
			content := []byte("app=\"kong-operator\"")
			tmpFile, err := os.CreateTemp("", "podinfo-labels-*")
			if err != nil {
				exitOnErr(fmt.Errorf("failed to create temporary file: %w", err))
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath)

			if _, err := tmpFile.Write(content); err != nil {
				tmpFile.Close()
				exitOnErr(fmt.Errorf("failed to write to temporary file: %w", err))
			}
			tmpFile.Close()

			// Try to copy the file to the target location
			if err := os.Rename(tmpPath, podLabelsFile); err != nil {
				// Permission denied, use sudo to copy
				fmt.Printf("INFO: using sudo to copy file to %s\n", podLabelsFile)
				cmd := exec.Command("sudo", "cp", tmpPath, podLabelsFile)
				if output, err := cmd.CombinedOutput(); err != nil {
					exitOnErr(fmt.Errorf("failed to copy file with sudo: %w, output: %s", err, string(output)))
				}
				// Set proper permissions
				cmd = exec.Command("sudo", "chmod", "644", podLabelsFile)
				if output, err := cmd.CombinedOutput(); err != nil {
					exitOnErr(fmt.Errorf("failed to set permissions with sudo: %w, output: %s", err, string(output)))
				}
			}

			fmt.Printf("INFO: successfully created pod labels file\n")
			// Verify the file was actually created and is readable
			if content, err := os.ReadFile(podLabelsFile); err != nil {
				exitOnErr(fmt.Errorf("failed to verify pod labels file %s: %w", podLabelsFile, err))
			} else {
				fmt.Printf("INFO: verified pod labels file content: %s\n", string(content))
			}
		}
	}

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
