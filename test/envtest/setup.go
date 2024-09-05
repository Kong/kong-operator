package envtest

import (
	"go/build"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TODO: extract the version from go.mod:
// https://github.com/Kong/gateway-operator/issues/548
const kongConfVersion = "v0.0.10"

// Setup sets up a test k8s API server environment and returned the configuration.
func Setup(t *testing.T, scheme *k8sruntime.Scheme) *rest.Config {
	t.Helper()

	testEnv := &envtest.Environment{
		ControlPlaneStopTimeout: time.Second * 60,
	}

	t.Logf("starting envtest environment for test %s...", t.Name())
	cfg, err := testEnv.Start()
	require.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(1)
	done := make(chan struct{})
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer wg.Done()
		select {
		case <-ch:
			_ = testEnv.Stop()
		case <-done:
			_ = testEnv.Stop()
		}
	}()

	kongCRDPath := filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "kong", "kubernetes-configuration@"+kongConfVersion, "config", "crd")
	kongBaseCRDPath := kongCRDPath + "/bases"
	// we do not deal with incubator resources here, so we only install base CRDs.
	t.Logf("install Kong CRDs from path %s", kongCRDPath)
	_, err = envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
		Scheme:             scheme,
		Paths:              []string{kongBaseCRDPath},
		ErrorIfPathMissing: true,
	})
	require.NoError(t, err)

	config, err := clientcmd.BuildConfigFromFlags(cfg.Host, "")
	require.NoError(t, err)
	config.CertData = cfg.CertData
	config.CAData = cfg.CAData
	config.KeyData = cfg.KeyData

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require.NoError(t, err)

	i, err := discoveryClient.ServerVersion()
	require.NoError(t, err)

	t.Logf("envtest environment (%s) started at %s", i, cfg.Host)

	t.Cleanup(func() {
		t.Helper()
		t.Logf("stopping envtest environment for test %s", t.Name())
		close(done)
		wg.Wait()
	})

	return cfg
}
