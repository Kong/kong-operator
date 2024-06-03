package conformance

import (
	"context"
	"fmt"
	"testing"

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
	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/gateway-operator/api/v1beta1"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/modules/manager/metadata"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
)

var skippedTestsForExpressionsRouter = []string{
	// gateway
	tests.GatewayInvalidTLSConfiguration.ShortName,

	// httproute
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,

	// TODO: remove the skip https://github.com/Kong/gateway-operator/issues/295
	// This test is flaky.
	tests.HTTPRouteWeight.ShortName,
}

var skippedTestsForTraditionalCompatibleRouter = []string{
	// gateway
	tests.GatewayInvalidTLSConfiguration.ShortName,

	// httproute
	tests.HTTPRouteHeaderMatching.ShortName,
	tests.HTTPRouteInvalidBackendRefUnknownKind.ShortName,

	// TODO: remove the skip https://github.com/Kong/gateway-operator/issues/295
	// This test is flaky.
	tests.HTTPRouteWeight.ShortName,
}

type ConformanceConfig struct {
	KongRouterFlavor RouterFlavor
}

func TestGatewayConformance(t *testing.T) {
	t.Parallel()

	// Conformance tests are run for both available router flavours:
	// traditional_compatible and expressions.
	var (
		config       ConformanceConfig
		skippedTests []string
	)
	switch rf := KongRouterFlavor(t); rf {
	case RouterFlavorTraditionalCompatible:
		skippedTests = skippedTestsForTraditionalCompatibleRouter
		config.KongRouterFlavor = RouterFlavorTraditionalCompatible
	case RouterFlavorExpressions:
		skippedTests = skippedTestsForExpressionsRouter
		config.KongRouterFlavor = RouterFlavorExpressions
	default:
		t.Fatalf("unsupported KongRouterFlavor: %s", rf)
	}

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
	reportFileName := fmt.Sprintf("kong-gateway-operator-%s.yaml", mode) // TODO: https://github.com/Kong/gateway-operator/issues/268

	opts := conformance.DefaultOptions(t)
	opts.ReportOutputPath = "../../" + reportFileName
	opts.Client = clients.MgrClient
	opts.GatewayClassName = gwc.Name
	opts.BaseManifests = testutils.GatewayRawRepoURL + "/conformance/base/manifests.yaml"
	opts.SkipTests = skippedTests
	opts.Mode = mode
	opts.ConformanceProfiles = sets.New(
		suite.GatewayHTTPConformanceProfileName,
	)
	opts.SupportedFeatures = sets.New(
		features.SupportHTTPRouteResponseHeaderModification,
	)
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

func createGatewayConfiguration(ctx context.Context, t *testing.T, c ConformanceConfig) *v1beta1.GatewayConfiguration {
	gwconf := v1beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kgo-gwconf-conformance-",
			Namespace:    "default",
		},
		Spec: v1beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &v1beta1.GatewayConfigDataPlaneOptions{
				Deployment: v1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: v1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "proxy",
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
			ControlPlaneOptions: &v1beta1.ControlPlaneOptions{
				Deployment: v1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "controller",
									ReadinessProbe: &corev1.Probe{
										InitialDelaySeconds: 1,
										PeriodSeconds:       1,
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("10m"),
											corev1.ResourceMemory: resource.MustParse("32Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("200m"),
											corev1.ResourceMemory: resource.MustParse("256Mi"),
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_LOG_LEVEL",
											Value: "debug",
										},
									},
									// TODO: https://github.com/Kong/gateway-operator/issues/294
									// change nightly to 3.2 when that's released
									Image: "kong/nightly-ingress-controller:2024-05-28",
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

func createGatewayClass(ctx context.Context, t *testing.T, gwconf *v1beta1.GatewayConfiguration) *gatewayv1.GatewayClass {
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
