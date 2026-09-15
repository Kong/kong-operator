package envtest

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	"github.com/kong/kong-operator/v2/ingress-controller/test/store"
	testutil "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

type Options struct {
	InstallGatewayCRDs bool
	InstallKongCRDs    bool
	AdditionalCRDPaths []string
}

var DefaultEnvTestOpts = Options{
	InstallGatewayCRDs: false,
	InstallKongCRDs:    true,
}

func optionsEqual(a, b Options) bool {
	return a.InstallGatewayCRDs == b.InstallGatewayCRDs &&
		a.InstallKongCRDs == b.InstallKongCRDs &&
		slices.Equal(a.AdditionalCRDPaths, b.AdditionalCRDPaths)
}

type OptionModifier func(Options) Options

func WithInstallKongCRDs(install bool) OptionModifier {
	return func(opts Options) Options {
		opts.InstallKongCRDs = install
		return opts
	}
}

func WithInstallGatewayCRDs(install bool) OptionModifier {
	return func(opts Options) Options {
		opts.InstallGatewayCRDs = install
		return opts
	}
}

func WithAdditionalCRDPaths(paths []string) OptionModifier {
	return func(opts Options) Options {
		opts.AdditionalCRDPaths = paths
		return opts
	}
}

const (
	// maxEnvtestStartAttempts bounds how many times we rebuild testEnv when the
	// control plane fails to start in time (e.g. the API server times out while
	// installing CRDs under load) or the webhook server's port was stolen between
	// allocation and bind (EADDRINUSE).
	maxEnvtestStartAttempts = 10
	// webhookBindProbe is how long we wait for ws.Start to surface a bind error
	// before treating the server as successfully serving. Binding is synchronous
	// and near-instant, so this only adds latency on the (common) success path.
	webhookBindProbe = 500 * time.Millisecond
	// controlPlaneStartTimeout bounds how long we wait for the API server/etcd to
	// come up and, when CRDs are configured, for them to be installed. The
	// controller-runtime default (20s) is too tight when many envtest environments
	// start concurrently in CI, so this is set generously.
	controlPlaneStartTimeout = 2 * time.Minute
	// crdInstallMaxTime bounds how long envtest waits for installed CRDs to become
	// established. Kept in step with controlPlaneStartTimeout for the same reason.
	crdInstallMaxTime = 2 * time.Minute
)

var once sync.Once = sync.Once{}

// Shared envtest environment support: opt-in via RunWithSharedEnvironment,
// used by packages (e.g. test/crdsvalidation/*) that only need installed CRDs
// and a fresh namespace per test, to avoid starting one etcd+API server pair
// per test. Guarded by sharedEnvMu rather than [sync.Once] so a failed
// creation attempt can be retried by the next test instead of poisoning the
// package.
var (
	sharedEnvMu      sync.Mutex
	sharedEnvEnabled bool
	sharedEnvOpts    Options
	sharedEnvCfg     *rest.Config
	sharedEnvStop    func()
)

// RunWithSharedEnvironment runs m.Run() against a single envtest environment
// shared by every test in the package, instead of starting a fresh
// etcd+API server pair (and reinstalling all CRDs) per test.
//
// Only use this for packages whose tests need nothing beyond installed CRDs
// and their own namespace (e.g. test/crdsvalidation/*): the environment and
// its installed CRDs are never reset between tests, and every Setup call in
// the package must request the same Options.
func RunWithSharedEnvironment(m *testing.M) int {
	sharedEnvEnabled = true
	code := m.Run()

	sharedEnvMu.Lock()
	stop := sharedEnvStop
	sharedEnvMu.Unlock()
	if stop != nil {
		stop()
	}

	return code
}

// makeStopper starts a goroutine that stops testEnv either when the process
// receives SIGINT/SIGTERM or when the returned stop func is called, and
// returns that stop func.
func makeStopper(testEnv *envtest.Environment) func() {
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
	return func() {
		close(done)
		wg.Wait()
	}
}

func createWebhookServer(scheme *runtime.Scheme, webhookOpts envtest.WebhookInstallOptions) webhook.Server {
	ws := webhook.NewServer(webhook.Options{
		Port:    webhookOpts.LocalServingPort,
		Host:    webhookOpts.LocalServingHost,
		CertDir: webhookOpts.LocalServingCertDir,
	})
	ws.Register("/convert", conversion.NewWebhookHandler(scheme, conversion.NewRegistry()))
	return ws
}

func createTestEnv(scheme *runtime.Scheme, crdPaths []string) *envtest.Environment {
	testEnv := &envtest.Environment{
		ControlPlaneStartTimeout: controlPlaneStartTimeout,
		ControlPlaneStopTimeout:  time.Second * 60,
		Scheme:                   scheme,
	}
	if len(crdPaths) > 0 {
		testEnv.CRDInstallOptions = envtest.CRDInstallOptions{
			Paths:              crdPaths,
			Scheme:             scheme,
			ErrorIfPathMissing: true,
			MaxTime:            crdInstallMaxTime,
		}
	}

	return testEnv
}

func generateCRDPaths(opts Options) []string {
	crdPaths := make([]string, 0, 3)
	if opts.InstallGatewayCRDs {
		crdPaths = append(crdPaths, kcfg.GatewayAPIExperimentalCRDsPath())
	}
	if opts.InstallKongCRDs {
		crdPaths = append(crdPaths,
			kcfg.KongOperatorCRDsPath(),
			kcfg.IngressControllerIncubatorCRDsPath(),
		)
	}
	crdPaths = append(crdPaths, opts.AdditionalCRDPaths...)
	return crdPaths
}

// startEnv starts a fresh envtest environment (control plane + conversion
// webhook server), retrying the whole environment when the control plane
// fails to start in time or the webhook server's allocated port gets stolen.
func startEnv(t *testing.T, ctx context.Context, scheme *runtime.Scheme, crdPaths []string) (*envtest.Environment, *rest.Config) {
	t.Helper()

	var (
		testEnv *envtest.Environment
		cfg     *rest.Config
	)
	for attempt := 1; ; attempt++ {
		testEnv = createTestEnv(scheme, crdPaths)
		t.Logf("starting envtest environment for test %s (attempt %d)...", t.Name(), attempt)
		var err error
		cfg, err = testEnv.Start()
		if err != nil {
			if attempt < maxEnvtestStartAttempts {
				t.Logf("envtest environment failed to start (%v); retrying with a fresh environment (attempt %d/%d)...", err, attempt+1, maxEnvtestStartAttempts)
				_ = testEnv.Stop()
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			require.NoError(t, err)
		}

		errCh := make(chan error, 1)
		go func() {
			ws := createWebhookServer(scheme, testEnv.WebhookInstallOptions)
			errCh <- ws.Start(ctx)
		}()

		select {
		case err := <-errCh:
			// ws.Start binds synchronously, so a fast return means it failed to bind.
			// The port envtest allocated can be stolen by another process between
			// allocation and bind; retry with a freshly allocated port in that case.
			if attempt < maxEnvtestStartAttempts && errors.Is(err, syscall.EADDRINUSE) {
				t.Logf("webhook server bind failed (%v); restarting envtest with a fresh port", err)
				_ = testEnv.Stop()
				continue
			}
			require.NoError(t, err)

		case <-time.After(webhookBindProbe):
			// The server is now serving (Start is blocking in Serve). Keep reporting
			// its eventual return error for the rest of the test run.
			go func() {
				assert.NoError(t, <-errCh)
			}()

		case <-ctx.Done():
			t.Fatalf("context canceled while waiting for webhook server to start: %v", ctx.Err())
		}
		break
	}

	return testEnv, cfg
}

// getSharedEnvConfig returns the package's shared envtest environment's
// rest.Config, creating it on first use. Guarded by sharedEnvMu so concurrent
// (t.Parallel()) callers don't race to create it, and so a failed creation
// attempt can be retried by whichever caller acquires the lock next.
func getSharedEnvConfig(t *testing.T, scheme *runtime.Scheme, opts Options) *rest.Config {
	t.Helper()

	sharedEnvMu.Lock()
	defer sharedEnvMu.Unlock()

	if sharedEnvCfg != nil {
		if !optionsEqual(opts, sharedEnvOpts) {
			t.Fatalf("envtest: shared environment was created with Options %+v, but this test requested %+v; "+
				"all tests in a package using RunWithSharedEnvironment must request the same Options", sharedEnvOpts, opts)
		}
		return sharedEnvCfg
	}

	// The shared webhook server must outlive the test that happens to create it,
	// so it can't be tied to that test's (cancelled-on-completion) context.
	testEnv, cfg := startEnv(t, context.Background(), scheme, generateCRDPaths(opts))
	sharedEnvStop = makeStopper(testEnv)
	sharedEnvOpts = opts
	sharedEnvCfg = cfg
	return cfg
}

// Setup sets up a test k8s API server environment and returned the configuration.
func Setup(t *testing.T, ctx context.Context, scheme *runtime.Scheme, optModifiers ...OptionModifier) (*rest.Config, *corev1.Namespace) {
	once.Do(func() {
		f, err := testutil.SetupControllerLogger("stdout")
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, f())
		})
	})

	t.Helper()

	opts := DefaultEnvTestOpts
	for _, mod := range optModifiers {
		opts = mod(opts)
	}

	var cfg *rest.Config
	if sharedEnvEnabled {
		cfg = rest.CopyConfig(getSharedEnvConfig(t, scheme, opts))
	} else {
		testEnv, envCfg := startEnv(t, ctx, scheme, generateCRDPaths(opts))
		stop := makeStopper(testEnv)
		t.Cleanup(func() {
			t.Helper()
			t.Logf("stopping envtest environment for test %s", t.Name())
			stop()
		})
		cfg = envCfg
	}

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
		GenerateName: "test-",
		Labels: map[string]string{
			"gateway-operator.konghq.com/test-name": NameFromT(t),
		},
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, cl.Create(ctx, ns))
	}, 30*time.Second, time.Second, "failed to create test namespace")

	return cfg, ns
}

func installGatewayCRDs(t *testing.T, scheme *runtime.Scheme, cfg *rest.Config) {
	t.Helper()
	_, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
		Scheme:             scheme,
		Paths:              []string{kcfg.GatewayAPIExperimentalCRDsPath()},
		ErrorIfPathMissing: true,
	})
	require.NoError(t, err, "failed installing Gateway API CRDs")
}

func deployIngressClass(ctx context.Context, t *testing.T, name string, client client.Client) {
	t.Helper()

	ingress := &netv1.IngressClass{
		Name: name,
		Spec: netv1.IngressClassSpec{
			Controller: store.IngressClassKongController,
		},
	}
	require.NoError(t, client.Create(ctx, ingress))
}

// NameFromT returns a name suitable for use in Kubernetes resources for a given test.
// This is used e.g. when setting kong addon's name.
func NameFromT(t *testing.T) string {
	t.Helper()

	name := strings.ToLower(t.Name())
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, "/", "-")

	// This is used as k8s label so we need to truncate it to 63.
	if len(name) > 63 {
		return name[:63]
	}

	return name
}
