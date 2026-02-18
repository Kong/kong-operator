package envtest

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
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

	"github.com/kong/kong-operator/v2/ingress-controller/test/controllers/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/store"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util/builder"
	testutil "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

type Options struct {
	InstallGatewayCRDs bool
	InstallKongCRDs    bool
}

var DefaultEnvTestOpts = Options{
	InstallGatewayCRDs: true,
	InstallKongCRDs:    true,
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

var once sync.Once = sync.Once{}

// Setup sets up a test k8s API server environment and returned the configuration.
func Setup(t *testing.T, ctx context.Context, scheme *k8sruntime.Scheme, optModifiers ...OptionModifier) (*rest.Config, *corev1.Namespace) {
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

	testEnv := &envtest.Environment{
		ControlPlaneStopTimeout: time.Second * 60,
		Scheme:                  scheme,
	}
	if len(crdPaths) > 0 {
		testEnv.CRDInstallOptions = envtest.CRDInstallOptions{
			Paths:              crdPaths,
			Scheme:             scheme,
			ErrorIfPathMissing: true,
			MaxTime:            30 * time.Second,
		}
	}

	t.Logf("starting envtest environment for test %s...", t.Name())
	cfg, err := testEnv.Start()
	require.NoError(t, err)

	ws := webhook.NewServer(webhook.Options{
		Port:    testEnv.WebhookInstallOptions.LocalServingPort,
		Host:    testEnv.WebhookInstallOptions.LocalServingHost,
		CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
	})
	ws.Register("/convert", conversion.NewWebhookHandler(scheme, conversion.NewRegistry()))
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

func installGatewayCRDs(t *testing.T, scheme *k8sruntime.Scheme, cfg *rest.Config) {
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
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: netv1.IngressClassSpec{
			Controller: store.IngressClassKongController,
		},
	}
	require.NoError(t, client.Create(ctx, ingress))
}

// deployGateway deploys a Gateway, GatewayClass, and ingress service for use in tests.
func deployGatewayUsingGatewayClass(ctx context.Context, t *testing.T, client client.Client, gwc gatewayapi.GatewayClass) gatewayapi.Gateway {
	ns := CreateNamespace(ctx, t, client)

	publishSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      PublishServiceName,
		},
		Spec: corev1.ServiceSpec{
			Ports: builder.NewServicePort().
				WithName("http").
				WithProtocol(corev1.ProtocolTCP).
				WithPort(8000).
				IntoSlice(),
		},
	}
	require.NoError(t, client.Create(ctx, &publishSvc))
	t.Cleanup(func() { _ = client.Delete(ctx, &publishSvc) })

	gw := gatewayapi.Gateway{
		Spec: gatewayapi.GatewaySpec{
			GatewayClassName: gatewayapi.ObjectName(gwc.Name),
			Listeners: []gatewayapi.Listener{
				{
					Name:          "http",
					Protocol:      gatewayapi.HTTPProtocolType,
					Port:          gatewayapi.PortNumber(8000),
					AllowedRoutes: builder.NewAllowedRoutesFromAllNamespaces(),
				},
			},
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      uuid.NewString(),
		},
	}
	require.NoError(t, client.Create(ctx, &gw))
	t.Cleanup(func() { _ = client.Delete(ctx, &gw) })

	return gw
}

func deployGateway(ctx context.Context, t *testing.T, client client.Client) (gatewayapi.Gateway, gatewayapi.GatewayClass) {
	gwc := gatewayapi.GatewayClass{
		Spec: gatewayapi.GatewayClassSpec{
			ControllerName: gateway.GetControllerName(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/gatewayclass-unmanaged": "placeholder",
			},
		},
	}
	require.NoError(t, client.Create(ctx, &gwc))
	t.Cleanup(func() { _ = client.Delete(ctx, &gwc) })

	gw := deployGatewayUsingGatewayClass(ctx, t, client, gwc)

	return gw, gwc
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
