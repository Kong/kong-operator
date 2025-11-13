package envtest

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	testutil "github.com/kong/kong-operator/pkg/utils/test"
)

var once sync.Once = sync.Once{}

// Setup sets up a test k8s API server environment and returned the configuration.
func Setup(t *testing.T, ctx context.Context, scheme *k8sruntime.Scheme) (*rest.Config, *corev1.Namespace) {
	once.Do(func() {
		f, err := testutil.SetupControllerLogger("stdout")
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, f())
		})
	})

	t.Helper()

	// GatewayAPIModuleName is the name of the module where we import and install Gateway API CRDs from.
	const gatewayAPIModuleName = "sigs.k8s.io/gateway-api"
	gwAPIVersion, err := testutil.ExtractModuleVersion(gatewayAPIModuleName)
	require.NoError(t, err)
	gwAPICRDPath := filepath.Join(
		testutil.ConstructModulePath(gatewayAPIModuleName, gwAPIVersion),
		"config", "crd", "standard",
	)

	// Use local CRDs from the repository instead of external module
	// Note: the local CRDs live under config/crd/kong-operator
	kongCRDPath := filepath.Join(testutil.ProjectRootPath(), "config", "crd", "kong-operator")

	testEnv := &envtest.Environment{
		ControlPlaneStopTimeout: time.Second * 60,
		Scheme:                  scheme,
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths: []string{
				gwAPICRDPath,
				kongCRDPath,
			},
			Scheme:             scheme,
			ErrorIfPathMissing: true,
			MaxTime:            30 * time.Second,
		},
	}

	t.Logf("starting envtest environment for test %s...", t.Name())
	cfg, err := testEnv.Start()
	require.NoError(t, err)

	ws := webhook.NewServer(webhook.Options{
		Port:    testEnv.WebhookInstallOptions.LocalServingPort,
		Host:    testEnv.WebhookInstallOptions.LocalServingHost,
		CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
	})
	ws.Register("/convert", conversion.NewWebhookHandler(scheme))
	go func() {
		require.NoError(t, ws.Start(ctx))
	}()

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

	config, err := clientcmd.BuildConfigFromFlags(cfg.Host, "")
	require.NoError(t, err)
	config.CertData = cfg.CertData
	config.CAData = cfg.CAData
	config.KeyData = cfg.KeyData
	config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(1_000, 10_000)

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	require.NoError(t, err)

	i, err := discoveryClient.ServerVersion()
	require.NoError(t, err)

	t.Logf("envtest environment (%s) started at %s", i, cfg.Host)

	cl, err := client.New(cfg, client.Options{})
	require.NoError(t, err)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Labels: map[string]string{
				"gateway-operator.konghq.com/test-name": NameFromT(t),
			},
		},
	}
	require.NoError(t, cl.Create(ctx, ns))

	t.Cleanup(func() {
		t.Helper()
		t.Logf("stopping envtest environment for test %s", t.Name())
		close(done)
		wg.Wait()
	})

	return cfg, ns
}

// NameFromT returns a name suitable for use in Kubernetes resources for a given test.
// This is used e.g. when setting kong addon's name.
func NameFromT(t *testing.T) string {
	t.Helper()

	name := strings.ToLower(t.Name())
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, "/", "-")

	return name
}
