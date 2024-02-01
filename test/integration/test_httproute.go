package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"
)

func init() {
	addTestsToTestSuite(
		TestHTTPRouteV1Beta1,
	)
}

func TestHTTPRouteV1Beta1(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying a GatewayConfiguration to set KIC log level")
	gatewayConfig := &operatorv1alpha1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1alpha1.GatewayConfigurationSpec{
			ControlPlaneOptions: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_LOG_LEVEL",
											Value: "trace",
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
	gatewayConfig, err := GetClients().OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv1alpha1.SchemeGroupVersion.Group),
				Kind:      gatewayv1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := testutils.GenerateGateway(gatewayNSN, gatewayClass)
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, testutils.GatewayIsScheduled(t, GetCtx(), gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	t.Log("verifying Gateway Listeners get marked as Programmed")
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, GetCtx(), gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, GetCtx(), gatewayNSN, clients)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(GetCtx(), deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(GetCtx(), service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("creating an httproute to access deployment %s via kong", deployment.Name)
	httpPort := gatewayv1.PortNumber(80)
	pathMatchPrefix := gatewayv1.PathMatchPathPrefix
	kindService := gatewayv1.Kind("Service")
	pathPrefix := "/prefix-test-http-route"
	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gateway.Name),
				}},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathMatchPrefix,
								Value: &pathPrefix,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(service.Name),
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
	httpRoute, err = GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).
		Create(GetCtx(), httpRoute, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(httpRoute)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, GetCtx(), clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route", "<title>httpbin.org</title>",
		),
		httpRouteAccessTimeout, time.Second,
	)
	// will route to path /1234 of service httpbin, but httpbin will return a 404 page on this path.
	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, GetCtx(), clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route/1234", "<h1>Not Found</h1>",
		),
		httpRouteAccessTimeout, time.Second,
	)
}
