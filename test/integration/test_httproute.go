package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestHTTPRouteV1Beta1(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying a GatewayConfiguration to set KIC log level")
	gatewayConfig := &operatorv1beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1beta1.GatewayConfigurationSpec{
			ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
	t.Logf("deploying GatewayConfiguration %s/%s to set KIC log level", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := GetClients().OperatorClient.ApisV1beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
				Kind:      gatewayv1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := testutils.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Scheduled", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsScheduled(t, GetCtx(), gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
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
	t.Logf("creating httproute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			httpRoute, err = GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(GetCtx(), httpRoute, metav1.CreateOptions{})
			if err != nil {
				t.Logf("failed to deploy httproute: %v", err)
				c.Errorf("failed to deploy httproute: %v", err)
			}
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)
	cleaner.Add(httpRoute)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, GetCtx(), clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route", http.MethodGet, "<title>httpbin.org</title>",
		),
		httpRouteAccessTimeout, time.Second,
	)
	// will route to path /1234 of service httpbin, but httpbin will return a 404 page on this path.
	require.Eventually(
		t, testutils.GetResponseBodyContains(
			t, GetCtx(), clients, httpc, "http://"+gatewayIPAddress+"/prefix-test-http-route/1234", http.MethodGet, "<h1>Not Found</h1>",
		),
		httpRouteAccessTimeout, time.Second,
	)
}
