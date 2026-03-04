package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance"
	conformancev1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	conformanceconfig "sigs.k8s.io/gateway-api/conformance/utils/config"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/pkg/consts"
	gatewayapipkg "github.com/kong/kong-operator/v2/pkg/gatewayapi"
	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	cleanupResources := !test.SkipCleanup()
	if cleanupResources {
		t.Cleanup(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: conformanceInfraNamespace}}
			err := clients.MgrClient.Delete(ctx, ns)
			if err != nil && !apierrors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})
	}

	// Conformance tests are run for both available router flavours:
	// traditional_compatible and expressions.
	kongRouterFlavor := KongRouterFlavor(t)
	switch kongRouterFlavor {
	case consts.RouterFlavorTraditionalCompatible, consts.RouterFlavorExpressions:
	default:
		t.Fatalf("unsupported KongRouterFlavor: %s", kongRouterFlavor)
	}

	supportedFeatures, err := gatewayapipkg.GetSupportedFeatures(kongRouterFlavor)
	require.NoError(t, err)

	gwType := gatewayType(test.ConformanceGatewayType())
	switch gwType {
	case standardGateway, hybridGateway:
	default:
		t.Fatalf("unsupported KONG_TEST_CONFORMANCE_GATEWAY_TYPE: %s", gwType)
	}

	if gwType == hybridGateway && test.KonnectAccessToken() == "" {
		t.Fatal("hybrid gateway type requires KONG_TEST_KONNECT_ACCESS_TOKEN to be set")
	}

	skippedTests := skippedTestsForConfig(kongRouterFlavor, gwType)
	runConformance(t, gwType, kongRouterFlavor, supportedFeatures, cleanupResources, skippedTests)
}

const conformanceLooserTimeout = 180 * time.Second

func runConformance(
	t *testing.T,
	gwType gatewayType,
	kongRouterFlavor consts.RouterFlavor,
	supportedFeatures sets.Set[features.FeatureName],
	cleanupResources bool,
	skipped []string,
) {
	t.Helper()
	ensureConformanceNamespace(ctx, t)

	if cleanupResources {
		t.Cleanup(func() {
			require.NoError(t, waitForConformanceGatewaysToCleanup(ctx, clients.GatewayClient.GatewayV1(), t.Logf))
			if gwType == hybridGateway {
				require.NoError(t, waitForConformanceKonnectGatewayControlPlanesToCleanup(ctx, t.Logf))
			}
		})
	}

	t.Logf("using the following Kong router flavor for the conformance tests: %s", kongRouterFlavor)
	t.Log("creating GatewayConfiguration and GatewayClass for gateway conformance tests")

	gwconf := createGatewayConfiguration(ctx, t, kongRouterFlavor, gwType)
	gwc := createGatewayClass(ctx, t, gwconf)

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Logf("configuring the Gateway API (%s) conformance test suite", gwType)
	// Currently mode only relies on the KongRouterFlavor, but in the future
	// we may want to add more modes.
	mode := string(kongRouterFlavor)
	md := metadata.Metadata()
	reportFileName := fmt.Sprintf("experimental-%s-%s-%s-report.yaml", md.Release, mode, gwType)

	// Set looser timeouts to avoid flakiness.
	timeoutConfig := conformanceconfig.DefaultTimeoutConfig()
	timeoutConfig.GatewayStatusMustHaveListeners = conformanceLooserTimeout
	timeoutConfig.GatewayListenersMustHaveConditions = conformanceLooserTimeout
	timeoutConfig.HTTPRouteMustHaveCondition = conformanceLooserTimeout

	opts := conformance.DefaultOptions(t)
	// It takes default conformance suite configuration manifests from provided location.
	opts.ManifestFS = kcfg.GatewayAPIConformanceTestsFilesystemsWithManifests()
	opts.ReportOutputPath = "../../" + reportFileName
	opts.Implementation = conformancev1.Implementation{
		Organization: md.Organization,
		Project:      md.ProjectName,
		URL:          md.RepoURL,
		Version:      md.Release,
		Contact: []string{
			md.RepoURL + "/issues/new/choose",
		},
	}
	opts.Mode = mode
	opts.ConformanceProfiles = sets.New(
		suite.GatewayHTTPConformanceProfileName,
		suite.GatewayGRPCConformanceProfileName,
	)
	opts.SupportedFeatures = supportedFeatures
	opts.SkipTests = skipped
	opts.CleanupBaseResources = cleanupResources
	opts.GatewayClassName = gwc.Name
	opts.Client = clients.MgrClient
	opts.TimeoutConfig = timeoutConfig
	opts.RestConfig.QPS = -1

	t.Log("running the Gateway API conformance test suite")
	conformance.RunConformanceWithOptions(t, opts)
}

type gatewayType string

const (
	standardGateway gatewayType = "standard"
	hybridGateway   gatewayType = "hybrid"
)

func ensureConformanceNamespace(ctx context.Context, t *testing.T) {
	t.Helper()

	nsKey := types.NamespacedName{Name: conformanceInfraNamespace}
	ns := &corev1.Namespace{}
	err := clients.MgrClient.Get(ctx, nsKey, ns)
	if err != nil && !apierrors.IsNotFound(err) {
		require.NoError(t, err)
	}
	if apierrors.IsNotFound(err) {
		testNamespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: conformanceInfraNamespace,
			},
		}
		err := clients.MgrClient.Create(ctx, &testNamespace)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			require.NoError(t, err)
		}
		return
	}

	if ns.DeletionTimestamp != nil {
    	t.Logf("namespace %s is terminating, waiting for deletion", conformanceInfraNamespace)
    	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
    		err := clients.MgrClient.Get(ctx, nsKey, &corev1.Namespace{})
    		if apierrors.IsNotFound(err) {
    			return true, nil
    		}
    		if err != nil {
    			return false, err
    		}
    		return false, nil
    	})
    	require.NoError(t, err)
    
    	testNamespace := corev1.Namespace{
    		ObjectMeta: metav1.ObjectMeta{
    			Name: conformanceInfraNamespace,
    		},
    	}
    	err = clients.MgrClient.Create(ctx, &testNamespace)
    	if err != nil && !apierrors.IsAlreadyExists(err) {
    		require.NoError(t, err)
    	}
	}
}

func createGatewayConfiguration(
	ctx context.Context, t *testing.T, kongRouterFlavor consts.RouterFlavor, gatewayType gatewayType,
) *operatorv2beta1.GatewayConfiguration {
	gwconf := operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ko-gwconf-conformance-",
			Namespace:    conformanceInfraNamespace,
		},
		Spec: operatorv2beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv2beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: consts.DataPlaneProxyContainerName,
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
										},
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("10m"),
												corev1.ResourceMemory: resource.MustParse("128Mi"),
											},
											Limits: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("500m"),
												corev1.ResourceMemory: resource.MustParse("1024Mi"),
											},
										},
										Env: []corev1.EnvVar{
											{
												Name:  "KONG_ROUTER_FLAVOR",
												Value: string(kongRouterFlavor),
											},
											// The test cases for GRPCRoute in the current GatewayAPI all use the h2c protocol.
											// In order to pass conformance tests, the proxy must listen http2 and http on the same port.
											{
												Name:  "KONG_PROXY_LISTEN",
												Value: "0.0.0.0:8000 http2, 0.0.0.0:8443 http2 ssl",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if gatewayType == hybridGateway {
		t.Log("configuring GatewayConfiguration with Konnect access token - Hybrid Gateway")
		kapi := konnectv1alpha1.KonnectAPIAuthConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "api-auth-config-",
				Namespace:    conformanceInfraNamespace,
			},
			Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
				Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
				Token:     test.KonnectAccessToken(),
				ServerURL: test.KonnectServerURL(),
			},
		}
		require.NoError(t, clients.MgrClient.Create(ctx, &kapi))
		t.Cleanup(func() {
			require.NoError(t, clients.MgrClient.Delete(ctx, &kapi))
		})

		gwconf.Spec.Konnect = &operatorv2beta1.KonnectOptions{
			APIAuthConfigurationRef: &v1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
				Name: kapi.Name,
			},
		}
	} else {
		t.Log("deploying GatewayConfiguration as a standard (non-hybrid) Gateway")
	}

	require.NoError(t, clients.MgrClient.Create(ctx, &gwconf))
	t.Cleanup(func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, &gwconf))
	})
	return &gwconf
}

func createGatewayClass(ctx context.Context, t *testing.T, gwconf *operatorv2beta1.GatewayConfiguration) *gatewayv1.GatewayClass {
	gwc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ko-gwclass-conformance-",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     "gateway-operator.konghq.com",
				Kind:      "GatewayConfiguration",
				Name:      gwconf.Name,
				Namespace: new(gwtypes.Namespace(gwconf.Namespace)),
			},
		},
	}
	require.NoError(t, clients.MgrClient.Create(ctx, gwc))
	t.Cleanup(func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, gwc))
	})

	return gwc
}
