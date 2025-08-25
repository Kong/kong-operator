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

	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayapipkg "github.com/kong/kong-operator/pkg/gatewayapi"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/pkg/vars"
)

var skippedTestsForExpressionsRouter = []string{}

var skippedTestsForTraditionalCompatibleRouter = []string{
	// httproute
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,
}

type ConformanceConfig struct {
	KongRouterFlavor consts.RouterFlavor
}

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	const looserTimeout = 180 * time.Second

	// Conformance tests are run for both available router flavours:
	// traditional_compatible and expressions.
	var (
		config            ConformanceConfig
		skippedTests      []string
		supportedFeatures sets.Set[features.FeatureName]
	)
	switch rf := KongRouterFlavor(t); rf {
	case consts.RouterFlavorTraditionalCompatible:
		skippedTests = skippedTestsForTraditionalCompatibleRouter
		config.KongRouterFlavor = consts.RouterFlavorTraditionalCompatible
	case consts.RouterFlavorExpressions:
		skippedTests = skippedTestsForExpressionsRouter
		config.KongRouterFlavor = consts.RouterFlavorExpressions
	default:
		t.Fatalf("unsupported KongRouterFlavor: %s", rf)
	}

	supportedFeatures, err := gatewayapipkg.GetSupportedFeatures(config.KongRouterFlavor)
	require.NoError(t, err)

	t.Logf("using the following configuration for the conformance tests: %+v", config)

	t.Log("creating GatewayConfiguration and GatewayClass for gateway conformance tests")
	gwconf := createGatewayConfiguration(ctx, t, config)
	gwc := createGatewayClass(ctx, t, gwconf)

	// There are no explicit conformance tests for GatewayClass, but we can
	// still run the conformance test suite setup to ensure that the
	// GatewayClass gets accepted.
	t.Log("configuring the Gateway API conformance test suite")
	// Currently mode only relies on the KongRouterFlavor, but in the future
	// we may want to add more modes.
	mode := string(config.KongRouterFlavor)
	metadata := metadata.Metadata()
	reportFileName := fmt.Sprintf("standard-%s-%s-report.yaml", metadata.Release, mode)

	// set looser timeouts to avoid flakiness
	timeoutConfig := conformanceconfig.DefaultTimeoutConfig()
	timeoutConfig.GatewayStatusMustHaveListeners = looserTimeout
	timeoutConfig.GatewayListenersMustHaveConditions = looserTimeout
	timeoutConfig.HTTPRouteMustHaveCondition = looserTimeout

	opts := conformance.DefaultOptions(t)
	opts.ReportOutputPath = "../../" + reportFileName
	opts.Client = clients.MgrClient
	opts.GatewayClassName = gwc.Name
	opts.TimeoutConfig = timeoutConfig
	opts.BaseManifests = testutils.GatewayRawRepoURL + "/conformance/base/manifests.yaml"
	opts.SkipTests = skippedTests
	opts.Mode = mode
	opts.ConformanceProfiles = sets.New(
		suite.GatewayHTTPConformanceProfileName,
	)
	opts.RestConfig.QPS = -1
	opts.SupportedFeatures = supportedFeatures
	opts.Implementation = conformancev1.Implementation{
		Organization: metadata.Organization,
		Project:      metadata.ProjectName,
		URL:          metadata.RepoURL,
		Version:      metadata.Release,
		Contact: []string{
			metadata.RepoURL + "/issues/new/choose",
		},
	}

	t.Log("running the Gateway API conformance test suite")
	conformance.RunConformanceWithOptions(t, opts)
}

func createGatewayConfiguration(ctx context.Context, t *testing.T, c ConformanceConfig) *operatorv2beta1.GatewayConfiguration {
	gwconf := operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kgo-gwconf-conformance-",
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
												Value: string(c.KongRouterFlavor),
											},
										},
									},
								},
							},
						},
					},
				},
			},

			// TODO(pmalek): add support for ControlPlane optionns using GatewayConfiguration v2
			// https://github.com/kong/kong-operator/issues/1728
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
			GenerateName: "kgo-gwclass-conformance-",
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
