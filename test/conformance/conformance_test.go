package conformance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance"
	conformancev1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/tests"
	conformanceconfig "sigs.k8s.io/gateway-api/conformance/utils/config"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayapipkg "github.com/kong/kong-operator/pkg/gatewayapi"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/pkg/vars"
	"github.com/kong/kong-operator/test"
)

var skippedTestsShared = []string{
	// TODO: https://github.com/Kong/kong-operator/issues/2215
	// HTTPRouteWeight is flaky for some reason. 2215 tracks solving it.
	tests.HTTPRouteWeight.ShortName,

	// NOTE:
	// Issue tracking all gRPC related conformance tests:
	// https://github.com/Kong/kong-operator/issues/2345
	// Tests GRPCRouteHeaderMatching, GRPCExactMethodMatching, GRPCRouteWeight are skipped
	// because to proxy different gRPC services and route requests based on Header or Method,
	// it is necessary to create separate catch-all routes for them.
	// However, Kong does not define priority behavior in this situation unless priorities are manually added.
	tests.GRPCRouteHeaderMatching.ShortName,
	tests.GRPCExactMethodMatching.ShortName,
	tests.GRPCRouteWeight.ShortName,
	// When processing this scenario, the Kong's router requires `priority` to be specified for routes.
	// We cannot provide that for routes that are part of the conformance suite.
	tests.GRPCRouteListenerHostnameMatching.ShortName,
}

var skippedTestsForExpressionsRouter = []string{}

var skippedTestsForTraditionalCompatibleRouter = []string{
	// HTTPRoute
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,
}

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	const looserTimeout = 180 * time.Second

	// Conformance tests are run for both available router flavours:
	// traditional_compatible and expressions.
	var (
		kongRouterFlavor  consts.RouterFlavor
		skippedTests      = skippedTestsShared
		supportedFeatures sets.Set[features.FeatureName]
	)
	switch rf := KongRouterFlavor(t); rf {
	case consts.RouterFlavorTraditionalCompatible:
		skippedTests = append(skippedTests, skippedTestsForTraditionalCompatibleRouter...)
		kongRouterFlavor = consts.RouterFlavorTraditionalCompatible
	case consts.RouterFlavorExpressions:
		skippedTests = append(skippedTests, skippedTestsForExpressionsRouter...)
		kongRouterFlavor = consts.RouterFlavorExpressions
	default:
		t.Fatalf("unsupported KongRouterFlavor: %s", rf)
	}

	supportedFeatures, err := gatewayapipkg.GetSupportedFeatures(kongRouterFlavor)
	require.NoError(t, err)

	t.Logf("using the following Kong router flavor for the conformance tests: %s", kongRouterFlavor)

	t.Log("creating GatewayConfiguration and GatewayClass for gateway conformance tests")
	gwconf := createGatewayConfiguration(ctx, t, kongRouterFlavor)
	gwc := createGatewayClass(ctx, t, gwconf)

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Log("configuring the Gateway API conformance test suite")
	// Currently mode only relies on the KongRouterFlavor, but in the future
	// we may want to add more modes.
	mode := string(kongRouterFlavor)
	metadata := metadata.Metadata()
	reportFileName := fmt.Sprintf("experimental-%s-%s-report.yaml", metadata.Release, mode)

	// Set looser timeouts to avoid flakiness.
	timeoutConfig := conformanceconfig.DefaultTimeoutConfig()
	timeoutConfig.GatewayStatusMustHaveListeners = looserTimeout
	timeoutConfig.GatewayListenersMustHaveConditions = looserTimeout
	timeoutConfig.HTTPRouteMustHaveCondition = looserTimeout

	opts := conformance.DefaultOptions(t)
	opts.BaseManifests = testutils.GatewayRawRepoURL + "/conformance/base/manifests.yaml"
	opts.ReportOutputPath = "../../" + reportFileName
	opts.Implementation = conformancev1.Implementation{
		Organization: metadata.Organization,
		Project:      metadata.ProjectName,
		URL:          metadata.RepoURL,
		Version:      metadata.Release,
		Contact: []string{
			metadata.RepoURL + "/issues/new/choose",
		},
	}
	opts.Mode = mode
	opts.ConformanceProfiles = sets.New(
		suite.GatewayHTTPConformanceProfileName,
		suite.GatewayGRPCConformanceProfileName,
	)
	opts.SupportedFeatures = supportedFeatures
	opts.SkipTests = skippedTests
	opts.CleanupBaseResources = test.SkipCleanup()
	opts.GatewayClassName = gwc.Name
	opts.Client = clients.MgrClient
	opts.TimeoutConfig = timeoutConfig
	opts.RestConfig.QPS = -1

	t.Log("running the Gateway API conformance test suite")
	conformance.RunConformanceWithOptions(t, opts)
}

func createGatewayConfiguration(ctx context.Context, t *testing.T, kongRouterFlavor consts.RouterFlavor) *operatorv2beta1.GatewayConfiguration {
	gwconf := operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ko-gwconf-conformance-",
			Namespace:    "default",
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
				Namespace: lo.ToPtr(gwtypes.Namespace(gwconf.Namespace)),
			},
		},
	}
	require.NoError(t, clients.MgrClient.Create(ctx, gwc))
	t.Cleanup(func() {
		require.NoError(t, clients.MgrClient.Delete(ctx, gwc))
	})

	return gwc
}
