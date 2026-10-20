package isolated

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	incubatorv1alpha1 "github.com/kong/kong-operator/v2/api/incubator/v1alpha1"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
	dpconf "github.com/kong/kong-operator/v2/ingress-controller/test/dataplane/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
	"github.com/kong/kong-operator/v2/modules/manager"
	mgrconfig "github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	kotest "github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/integration"
	kicconsts "github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

const (
	// controlPlaneDiagnosticsAddr is the address of the operator's diagnostics server
	// that exposes the configuration dumps of the ControlPlanes it manages.
	// The operator runs in the test process, so it's reachable on localhost.
	controlPlaneDiagnosticsAddr = "localhost:10256"

	// gatewayHTTPListenerName and gatewayHTTPSListenerName are the names of the listeners
	// of the Gateway created by featureSetup when no listeners are specified.
	gatewayHTTPListenerName  = "http"
	gatewayHTTPSListenerName = "https"
)

// tenv runs the e2e-framework features of this package.
var tenv = &lazyEnv{}

// lazyEnv defers creating the e2e-framework environment until the first test runs,
// as the test cluster is created by integration.Suite right before it runs the tests.
type lazyEnv struct {
	once sync.Once
	cfg  *envconf.Config
	env  env.Environment
	err  error
}

// Test runs the given features in the environment.
func (l *lazyEnv) Test(t *testing.T, fs ...features.Feature) {
	t.Helper()

	l.once.Do(func() {
		cluster := integration.GetEnv().Cluster()
		c, err := klient.New(cluster.Config())
		if err != nil {
			l.err = fmt.Errorf("failed to create e2e-framework client: %w", err)
			return
		}
		// Specifying a run ID so that multiple runs wouldn't collide.
		// It is used when creating tests namespaces and their labels.
		runID := envconf.RandomName("", 3)
		fmt.Printf("INFO: runID %s\n", runID)

		ctx := SetClusterInCtx(integration.GetCtx(), cluster)
		ctx = SetRunIDInCtx(ctx, runID)
		l.env = env.NewWithConfig(l.cfg.WithClient(c)).WithContext(ctx)
	})
	if l.err != nil {
		t.Fatal(l.err)
	}

	l.env.Test(t, fs...)
}

// -----------------------------------------------------------------------------
// Testing Main
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	cfg, err := envconf.NewFromFlags()
	if err != nil {
		fmt.Printf("ERROR: failed to parse e2e-framework flags: %v\n", err)
		os.Exit(kicconsts.ExitCodeEnvSetupFailed)
	}
	tenv.cfg = cfg

	opts := []integration.SuiteOption{
		integration.WithControllerConfigOptions(func(c *manager.Config) {
			c.ControlPlaneConfigurationDumpEnabled = true
			c.ControlPlaneConfigurationDumpAddr = controlPlaneDiagnosticsAddr
		}),
	}

	// Apply a KongLicense shared by all tests. KongLicense is cluster-scoped, so every
	// ControlPlane programs it into its DataPlane.
	// Only for Kong Enterprise: when recovering from a sync error, the controller adds
	// the license to the last valid config regardless of the Kong edition, and Kong OSS
	// rejects config with a license in it.
	switch {
	case !kongEnterpriseUsed():
		fmt.Println("INFO: Kong Enterprise not used, skipping KongLicense creation")
	case kotest.KongLicenseData() == "":
		fmt.Println("INFO: KONG_LICENSE_DATA not set, skipping KongLicense creation")
	default:
		opts = append(opts, integration.WithKongLicense())
	}

	integration.Suite(m, opts...)
}

// -----------------------------------------------------------------------------
// Feature setup
// -----------------------------------------------------------------------------

type featureSetupCfg struct {
	controlPlaneFeatureGates map[string]string
	useLastValidConfig       bool
	kongRouterFlavor         dpconf.RouterFlavor
	gatewayListeners         []gatewayapi.Listener
	skipGateway              bool
}

type featureSetupOpt func(*featureSetupCfg)

// withControlPlaneFeatureGates sets feature gates (name => "true"/"false") of the
// ControlPlane created for the test's Gateway, on top of the defaults.
func withControlPlaneFeatureGates(gates map[string]string) featureSetupOpt {
	return func(o *featureSetupCfg) {
		o.controlPlaneFeatureGates = gates
	}
}

// withUseLastValidConfigForFallback makes the ControlPlane use the last valid configuration
// to fill in the entities excluded by the fallback configuration.
func withUseLastValidConfigForFallback() featureSetupOpt {
	return func(o *featureSetupCfg) {
		o.useLastValidConfig = true
	}
}

// withKongRouterFlavor sets the router flavor of the DataPlane, regardless of TEST_KONG_ROUTER_FLAVOR.
func withKongRouterFlavor(flavor dpconf.RouterFlavor) featureSetupOpt {
	return func(o *featureSetupCfg) {
		o.kongRouterFlavor = flavor
	}
}

// withGatewayListeners sets the listeners of the Gateway created for the test.
// By default, the Gateway gets an HTTP listener on port 80 and an HTTPS listener on port 443.
func withGatewayListeners(listeners ...gatewayapi.Listener) featureSetupOpt {
	return func(o *featureSetupCfg) {
		o.gatewayListeners = listeners
	}
}

// withoutGateway makes featureSetup not create a Gateway. Use it for tests that bring their own
// Gateway (e.g. by applying a sample manifest) and call waitForGateway themselves.
func withoutGateway() featureSetupOpt {
	return func(o *featureSetupCfg) {
		o.skipGateway = true
	}
}

// featureSetup creates the test's namespace and, unless withoutGateway is used, a Gateway managed
// by the operator, which provisions a DataPlane and a ControlPlane for it. It stores in the context:
// - the namespace (GetNamespaceForT) and the cleaner,
// - the ingress class of the ControlPlane (GetIngressClassFromCtx),
// - the Gateway (GetFromCtxForT[*gatewayapi.Gateway]) and the proxy URLs of its listeners,
// - the operator's diagnostics server URL (GetDiagURLFromCtx).
func featureSetup(opts ...featureSetupOpt) func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
	var setupCfg featureSetupCfg
	for _, opt := range opts {
		opt(&setupCfg)
	}
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		ctx = setUpNamespaceAndCleaner(ctx, t, cfg)

		cluster := GetClusterFromCtx(ctx)
		ingressClass := envconf.RandomName("ingressclass", 16)
		t.Logf("deploying the ControlPlane's IngressClass %q", ingressClass)
		if !assert.NoError(t, helpers.CreateIngressClass(ctx, ingressClass, cluster.Client()), "failed creating IngressClass") {
			return ctx
		}
		t.Cleanup(func() {
			// deleting this directly instead of adding it to the cleaner because
			// the cleaner always gets a 404 on it for unknown reasons
			_ = cluster.Client().NetworkingV1().IngressClasses().Delete(context.Background(), ingressClass, metav1.DeleteOptions{})
		})
		ctx = setInCtx(ctx, _ingressClass{}, ingressClass)
		ctx = SetDiagURLInCtx(ctx, &url.URL{Scheme: "http", Host: controlPlaneDiagnosticsAddr})

		if setupCfg.skipGateway {
			return ctx
		}
		return setUpGateway(ctx, t, opts...)
	}
}

// setUpGateway deploys a Gateway managed by the operator for the test, configured with the given options
// (as in featureSetup), and waits for it to be ready (see waitForGateway). It's meant for tests that need to
// create some resources before the Gateway, so they use featureSetup(withoutGateway()) and call it afterwards.
func setUpGateway(ctx context.Context, t *testing.T, opts ...featureSetupOpt) context.Context {
	t.Helper()

	var setupCfg featureSetupCfg
	for _, opt := range opts {
		opt(&setupCfg)
	}
	ctx, gatewayNN, ok := deployGateway(ctx, t, setupCfg, GetIngressClassFromCtx(ctx))
	if !ok {
		return ctx
	}
	return waitForGateway(ctx, t, gatewayNN)
}

// setUpNamespaceAndCleaner creates the namespace to run the test and the cleaner to clean up created resources.
func setUpNamespaceAndCleaner(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	// TODO: this is temporary to allow things like:
	// clusters.ApplyManifestByYAML(ctx, cluster, s)
	// in tests.
	// Ideally this wouldn't be possible because it allows tests to break
	// a shared cluster but we don't have means to use kubectl against a cluster
	// without including a 3rd party package.
	// We should remove the cluster from the context as the last setup step.
	cluster := GetClusterFromCtx(ctx)

	runID := GetRunIDFromCtx(ctx)

	ctx, err := CreateNSForTest(ctx, cfg, t, runID)
	if !assert.NoError(t, err) {
		return ctx
	}

	t.Log("Create a cluster cleaner")
	cleaner := clusters.NewCleaner(cluster, cleanerScheme())
	t.Cleanup(func() {
		helpers.DumpDiagnosticsIfFailed(ctx, t, cluster)
		t.Logf("Start cleanup for test %s", t.Name())
		if err := cleaner.Cleanup(context.Background()); err != nil { //nolint:contextcheck
			fmt.Printf("ERROR: failed cleaning up the cluster: %v\n", err)
		}
	})
	ctx = SetInCtxForT(ctx, t, cleaner)

	return ctx
}

// cleanerScheme returns a scheme aware of all the types the tests create.
func cleanerScheme() *runtime.Scheme {
	s := scheme.Get()
	utilruntime.Must(incubatorv1alpha1.AddToScheme(s))
	return s
}

// dataPlaneImage returns the Kong image to use for DataPlanes, TEST_KONG_IMAGE:TEST_KONG_TAG if set.
func dataPlaneImage() string {
	if image := testenv.KongImageTag(); image != "" {
		return image
	}
	return helpers.GetDefaultDataPlaneImage()
}

// deployGateway deploys a GatewayConfiguration, a GatewayClass and a Gateway for the test.
// It stores the GatewayClass in the context (GetFromCtxForT[*gatewayapi.GatewayClass]) and returns
// the Gateway's namespaced name, or false when the deployment failed.
func deployGateway(
	ctx context.Context, t *testing.T, setupCfg featureSetupCfg, ingressClass string,
) (context.Context, k8stypes.NamespacedName, bool) {
	t.Helper()

	cleaner := GetFromCtxForT[*clusters.Cleaner](ctx, t)
	namespace := GetNamespaceForT(ctx, t)
	clients := integration.GetClients()

	listeners := setupCfg.gatewayListeners
	if listeners == nil {
		cert, key := certificate.MustGenerateCertPEMFormat()
		secret, ok := deployCertificateSecret(ctx, t, "gateway-https-cert", cert, key)
		if !ok {
			return ctx, k8stypes.NamespacedName{}, false
		}
		listeners = []gatewayapi.Listener{
			{
				Name:     gatewayHTTPListenerName,
				Protocol: gatewayapi.HTTPProtocolType,
				Port:     80,
			},
			{
				Name:     gatewayHTTPSListenerName,
				Protocol: gatewayapi.HTTPSProtocolType,
				Port:     443,
				TLS: &gatewayapi.GatewayTLSConfig{
					CertificateRefs: []gatewayapi.SecretObjectReference{
						{Name: gatewayapi.ObjectName(secret.Name)},
					},
				},
			},
		}
	}

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace, func(gc *operatorv2beta1.GatewayConfiguration) {
		container := k8sutils.GetPodContainerByName(
			&gc.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName,
		)
		container.Image = dataPlaneImage()
		routerFlavor := setupCfg.kongRouterFlavor
		if routerFlavor == "" {
			routerFlavor = testenv.KongRouterFlavor()
		}
		if routerFlavor != "" {
			container.Env = append(container.Env, corev1.EnvVar{Name: consts.RouterFlavorEnvKey, Value: string(routerFlavor)})
		}

		featureGates := map[string]string{
			managercfg.KongServiceFacadeFeature: "true",
			managercfg.KongCustomEntityFeature:  "true",
		}
		maps.Copy(featureGates, setupCfg.controlPlaneFeatureGates)
		cpOpts := operatorv2beta1.ControlPlaneOptions{
			IngressClass: new(ingressClass),
			FeatureGates: lo.MapToSlice(featureGates, func(name, value string) operatorv2beta1.ControlPlaneFeatureGate {
				state := operatorv2beta1.FeatureGateStateDisabled
				if value == "true" {
					state = operatorv2beta1.FeatureGateStateEnabled
				}
				return operatorv2beta1.ControlPlaneFeatureGate{Name: name, State: state}
			}),
			ConfigDump: &operatorv2beta1.ControlPlaneConfigDump{
				State:         operatorv2beta1.ConfigDumpStateEnabled,
				DumpSensitive: operatorv2beta1.ConfigDumpStateEnabled,
			},
		}
		if setupCfg.useLastValidConfig {
			cpOpts.Translation = &operatorv2beta1.ControlPlaneTranslationOptions{
				FallbackConfiguration: &operatorv2beta1.ControlPlaneFallbackConfiguration{
					UseLastValidConfig: new(operatorv2beta1.ControlPlaneFallbackConfigurationStateEnabled),
				},
			}
		}
		gc.Spec.ControlPlaneOptions = &operatorv2beta1.GatewayConfigControlPlaneOptions{ControlPlaneOptions: cpOpts}
	})
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := clients.OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	if !assert.NoError(t, err) {
		return ctx, k8stypes.NamespacedName{}, false
	}
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = clients.GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	if !assert.NoError(t, err) {
		return ctx, k8stypes.NamespacedName{}, false
	}
	cleaner.Add(gatewayClass)
	ctx = SetInCtxForT(ctx, t, gatewayClass)

	gatewayNN := k8stypes.NamespacedName{Namespace: namespace, Name: uuid.NewString()}
	gateway := helpers.GenerateGateway(gatewayNN, gatewayClass, func(gw *gatewayv1.Gateway) {
		gw.Spec.Listeners = listeners
	})
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = clients.GatewayClient.GatewayV1().Gateways(namespace).Create(ctx, gateway, metav1.CreateOptions{})
	if !assert.NoError(t, err) {
		return ctx, k8stypes.NamespacedName{}, false
	}
	cleaner.Add(gateway)

	return ctx, gatewayNN, true
}

// deployCertificateSecret deploys a TLS Secret with the given PEM encoded certificate and key,
// labeled so that the operator's ControlPlanes ingest it.
func deployCertificateSecret(ctx context.Context, t *testing.T, name string, cert, key []byte) (*corev1.Secret, bool) {
	t.Helper()

	secret := &corev1.Secret{
		Name: name,
		Labels: map[string]string{
			mgrconfig.DefaultSecretLabelSelector: mgrconfig.LabelValueForSelectorTrue,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	secret, err := GetClusterFromCtx(ctx).Client().CoreV1().Secrets(GetNamespaceForT(ctx, t)).Create(ctx, secret, metav1.CreateOptions{})
	if !assert.NoError(t, err) {
		return nil, false
	}
	GetFromCtxForT[*clusters.Cleaner](ctx, t).Add(secret)
	return secret, true
}

// controlPlaneRef is the namespaced name of the ControlPlane managed for the test's Gateway.
type controlPlaneRef k8stypes.NamespacedName

// waitForGateway waits for the Gateway to get Programmed and an address assigned. Then it stores
// in the context the Gateway, the proxy URLs of its listeners and the ControlPlane managed for it.
// The URLs point at the first listener of the respective protocol:
// GetHTTPURLFromCtx, GetHTTPSURLFromCtx, GetTCPURLFromCtx, GetUDPURLFromCtx and GetTLSURLFromCtx.
func waitForGateway(ctx context.Context, t *testing.T, gatewayNN k8stypes.NamespacedName) context.Context {
	t.Helper()

	cl := integration.GetClients().MgrClient

	t.Logf("waiting for Gateway %s to be Programmed and get an address", gatewayNN)
	var gateway gatewayapi.Gateway
	if !assert.EventuallyWithT(t, func(c *assert.CollectT) {
		if !assert.NoError(c, cl.Get(ctx, gatewayNN, &gateway)) {
			return
		}
		assert.True(c, gatewayutils.IsProgrammed(&gateway), "Gateway is not Programmed")
		assert.NotEmpty(c, gateway.Status.Addresses, "Gateway has no addresses")
	}, kicconsts.IngressWait, kicconsts.WaitTick) {
		return ctx
	}
	ctx = SetInCtxForT(ctx, t, &gateway)

	address := gateway.Status.Addresses[0].Value
	t.Logf("Gateway %s got address %s", gatewayNN, address)
	ctx = setGatewayListenersURLsInCtx(ctx, address, gateway.Spec.Listeners)

	t.Logf("waiting for the ControlPlane of Gateway %s", gatewayNN)
	var controlPlaneNN k8stypes.NamespacedName
	if !assert.EventuallyWithT(t, func(c *assert.CollectT) {
		controlPlanes, err := gatewayutils.ListControlPlanesForGateway(ctx, cl, &gateway)
		if !assert.NoError(c, err) || !assert.Len(c, controlPlanes, 1) {
			return
		}
		controlPlaneNN = k8stypes.NamespacedName{Namespace: controlPlanes[0].Namespace, Name: controlPlanes[0].Name}
	}, kicconsts.IngressWait, kicconsts.WaitTick) {
		return ctx
	}
	return SetInCtxForT(ctx, t, controlPlaneRef(controlPlaneNN))
}

// waitForDataPlane waits for a standalone DataPlane (not managed for a Gateway) to get Ready and an address.
// Then it stores in the context the proxy URLs of its default HTTP (80) and HTTPS (443) ports.
func waitForDataPlane(ctx context.Context, t *testing.T, dataPlaneNN k8stypes.NamespacedName) context.Context {
	t.Helper()

	cl := integration.GetClients().MgrClient

	t.Logf("waiting for DataPlane %s to be Ready and get an address", dataPlaneNN)
	var dataPlane operatorv1beta1.DataPlane
	if !assert.EventuallyWithT(t, func(c *assert.CollectT) {
		if !assert.NoError(c, cl.Get(ctx, dataPlaneNN, &dataPlane)) {
			return
		}
		ready, ok := lo.Find(dataPlane.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == string(kcfgdataplane.ReadyType)
		})
		assert.True(c, ok && ready.Status == metav1.ConditionTrue, "DataPlane is not Ready")
		assert.NotEmpty(c, dataPlane.Status.Addresses, "DataPlane has no addresses")
	}, kicconsts.IngressWait, kicconsts.WaitTick) {
		return ctx
	}

	address := dataPlane.Status.Addresses[0].Value
	t.Logf("DataPlane %s got address %s", dataPlaneNN, address)
	return setGatewayListenersURLsInCtx(ctx, address, []gatewayapi.Listener{
		{Protocol: gatewayapi.HTTPProtocolType, Port: consts.DefaultHTTPPort},
		{Protocol: gatewayapi.HTTPSProtocolType, Port: consts.DefaultHTTPSPort},
	})
}

// setGatewayListenersURLsInCtx stores in the context the proxy URLs of the first listener
// of each protocol of a Gateway reachable at the given address.
func setGatewayListenersURLsInCtx(ctx context.Context, address string, listeners []gatewayapi.Listener) context.Context {
	firstListenerAddr := func(protocol gatewayapi.ProtocolType) (string, bool) {
		l, ok := lo.Find(listeners, func(l gatewayapi.Listener) bool { return l.Protocol == protocol })
		if !ok {
			return "", false
		}
		return helpers.HostPort(address, int(l.Port)), true
	}
	if addr, ok := firstListenerAddr(gatewayapi.HTTPProtocolType); ok {
		ctx = SetHTTPURLInCtx(ctx, &url.URL{Scheme: "http", Host: addr})
	}
	if addr, ok := firstListenerAddr(gatewayapi.HTTPSProtocolType); ok {
		ctx = SetHTTPSURLInCtx(ctx, &url.URL{Scheme: "https", Host: addr})
	}
	if addr, ok := firstListenerAddr(gatewayapi.TCPProtocolType); ok {
		ctx = SetTCPURLInCtx(ctx, addr)
	}
	if addr, ok := firstListenerAddr(gatewayapi.UDPProtocolType); ok {
		ctx = SetUDPURLInCtx(ctx, addr)
	}
	if addr, ok := firstListenerAddr(gatewayapi.TLSProtocolType); ok {
		ctx = SetTLSURLInCtx(ctx, addr)
	}
	return ctx
}

// deleteGatewayClass deletes the GatewayClass and waits until it's gone.
func deleteGatewayClass(ctx context.Context, t *testing.T, gatewayClass *gatewayapi.GatewayClass) {
	t.Helper()

	cl := integration.GetClients().MgrClient
	t.Logf("deleting GatewayClass %s", gatewayClass.Name)
	assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, gatewayClass.DeepCopy())))
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		err := cl.Get(ctx, client.ObjectKeyFromObject(gatewayClass), &gatewayapi.GatewayClass{})
		assert.True(c, apierrors.IsNotFound(err), "GatewayClass still exists (err: %v)", err)
	}, kicconsts.IngressWait, kicconsts.WaitTick)
}

// recreateGatewayClass creates the deleted GatewayClass again, with the same name and spec.
func recreateGatewayClass(ctx context.Context, t *testing.T, gatewayClass *gatewayapi.GatewayClass) {
	t.Helper()

	t.Logf("creating GatewayClass %s again", gatewayClass.Name)
	assert.NoError(t, integration.GetClients().MgrClient.Create(ctx, &gatewayapi.GatewayClass{
		Name: gatewayClass.Name,
		Spec: *gatewayClass.Spec.DeepCopy(),
	}))
}

// deleteGateway deletes the Gateway and waits until it's gone. The operator removes
// the Gateway's DataPlane and ControlPlane before letting it go (finalizers).
func deleteGateway(ctx context.Context, t *testing.T, gateway *gatewayapi.Gateway) {
	t.Helper()

	cl := integration.GetClients().MgrClient
	t.Logf("deleting Gateway %s/%s", gateway.Namespace, gateway.Name)
	assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, gateway.DeepCopy())))
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		err := cl.Get(ctx, client.ObjectKeyFromObject(gateway), &gatewayapi.Gateway{})
		assert.True(c, apierrors.IsNotFound(err), "Gateway still exists (err: %v)", err)
	}, kicconsts.IngressWait, kicconsts.WaitTick)
}

// recreateGateway creates the deleted Gateway again, with the same name and spec, and waits for it
// to be ready (see waitForGateway). Its address, and so the proxy URLs, might differ from the previous ones.
func recreateGateway(ctx context.Context, t *testing.T, gateway *gatewayapi.Gateway) context.Context {
	t.Helper()

	t.Logf("creating Gateway %s/%s again", gateway.Namespace, gateway.Name)
	if !assert.NoError(t, integration.GetClients().MgrClient.Create(ctx, &gatewayapi.Gateway{
		Namespace: gateway.Namespace,
		Name:      gateway.Name,
		Spec:      *gateway.Spec.DeepCopy(),
	})) {
		return ctx
	}
	return waitForGateway(ctx, t, client.ObjectKeyFromObject(gateway))
}

// controlPlaneDiagnosticsPath returns the path of the given diagnostics endpoint (e.g. "/fallback")
// of the test Gateway's ControlPlane, to be used with the URL from GetDiagURLFromCtx.
func controlPlaneDiagnosticsPath(ctx context.Context, t *testing.T, endpoint string) string {
	t.Helper()

	cp := GetFromCtxForT[controlPlaneRef](ctx, t)
	return fmt.Sprintf("/debug/controlplanes/namespace/%s/name/%s/config%s", cp.Namespace, cp.Name, endpoint)
}

func featureTeardown() func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		runID := GetRunIDFromCtx(ctx)
		ctx, err := deleteNSForTest(ctx, c, t, runID)
		assert.NoError(t, err)
		return ctx
	}
}
