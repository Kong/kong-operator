//go:build integration_tests
// +build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
)

func TestHTTPRouteV1Beta1(t *testing.T) {
	namespace, cleaner := setup(t, ctx, env, clients)
	defer func() {
		assert.NoError(t, cleaner.Cleanup(ctx))
	}()

	t.Log("deploying a GatewayConfiguration to set KIC log level")
	gatewayConfig := &operatorv1alpha1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1alpha1.GatewayConfigurationSpec{
			ControlPlaneDeploymentOptions: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "CONTROLLER_LOG_LEVEL", Value: "trace"},
					},
				},
			},
		},
	}
	gatewayConfig, err := clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ParametersRef: &gatewayv1beta1.ParametersReference{
				Group:     gatewayv1beta1.Group(operatorv1alpha1.SchemeGroupVersion.Group),
				Kind:      gatewayv1beta1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1beta1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName),
		},
	}
	gatewayClass, err = clients.GatewayClient.GatewayV1beta1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := testutils.GenerateGateway(gatewayNSN, gatewayClass)
	gateway, err = clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsScheduled(t, ctx, gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Ready")
	require.Eventually(t, testutils.GatewayIsReady(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = env.Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("creating an httproute to access deployment %s via kong", deployment.Name)
	httpPort := gatewayv1beta1.PortNumber(80)
	pathMatchPrefix := gatewayv1beta1.PathMatchPathPrefix
	kindService := gatewayv1beta1.Kind("Service")
	pathPrefix := "/prefix-test-http-route"
	httpRoute := &gatewayv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
		},
		Spec: gatewayv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1beta1.CommonRouteSpec{
				ParentRefs: []gatewayv1beta1.ParentReference{{
					Name: gatewayv1beta1.ObjectName(gateway.Name),
				}},
			},
			Rules: []gatewayv1beta1.HTTPRouteRule{
				{
					Matches: []gatewayv1beta1.HTTPRouteMatch{
						{
							Path: &gatewayv1beta1.HTTPPathMatch{
								Type:  &pathMatchPrefix,
								Value: &pathPrefix,
							},
						},
					},
					BackendRefs: []gatewayv1beta1.HTTPBackendRef{
						{
							BackendRef: gatewayv1beta1.BackendRef{
								BackendObjectReference: gatewayv1beta1.BackendObjectReference{
									Name: gatewayv1beta1.ObjectName(service.Name),
									Port: &httpPort,
									Kind: &kindService,
								},
							},
						},
					},
				},
			},
		},
	}
	httpRoute, err = clients.GatewayClient.GatewayV1beta1().HTTPRoutes(namespace.Name).
		Create(ctx, httpRoute, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(httpRoute)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, ctx, clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route", "<title>httpbin.org</title>",
		),
		httpRouteAccessTimeout, time.Second,
	)
	// will route to path /1234 of service httpbin, but httpbin will return a 404 page on this path.
	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, ctx, clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route/1234", "<h1>Not Found</h1>",
		),
		httpRouteAccessTimeout, time.Second,
	)
}
