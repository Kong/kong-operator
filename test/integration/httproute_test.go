package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/v2/modules/manager/config"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestHTTPRoute(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Accepted", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, GetCtx(), gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

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

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name)
	t.Logf("creating httproute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(GetCtx(), httpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy httproute %s/%s", httpRoute.Namespace, httpRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	request := helpers.MustBuildRequest(t, GetCtx(), http.MethodGet, "http://"+gatewayIPAddress+"/test", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, clients, httpClient, request, "<title>httpbin.org</title>"),
		httpRouteAccessTimeout,
		time.Second,
	)

	t.Log("route to /test/1234 path of service httpbin should receive a 404 OK response")
	request = helpers.MustBuildRequest(t, GetCtx(), http.MethodGet, "http://"+gatewayIPAddress+"/test/1234", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, clients, httpClient, request, "<h1>Not Found</h1>"),
		httpRouteAccessTimeout,
		time.Second,
	)
}

func TestHTTPRouteWithTLS(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	const host = "integration.tests.org"
	cert, key := certificate.MustGenerateSelfSignedCertPEMFormat(certificate.WithDNSNames(host))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      host,
			Labels: map[string]string{
				config.DefaultSecretLabelSelector: config.LabelValueForSelectorTrue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	t.Logf("deploying Secret %s/%s", secret.Namespace, secret.Name)
	secret, err = GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(GetCtx(), secret, metav1.CreateOptions{})
	require.NoError(t, err)

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, func(gateway *gatewayv1.Gateway) {
		gateway.Spec.Listeners[0].Protocol = gatewayv1.HTTPSProtocolType
		gateway.Spec.Listeners[0].Port = gatewayv1.PortNumber(443)
		gateway.Spec.Listeners[0].TLS = &gatewayv1.GatewayTLSConfig{
			CertificateRefs: []gatewayv1.SecretObjectReference{
				{
					Name:      gatewayv1.ObjectName(secret.Name),
					Namespace: lo.ToPtr(gatewayv1.Namespace(secret.Namespace)),
				},
			},
		}
	})

	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Scheduled", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, GetCtx(), gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, GetCtx(), gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIPAddressExist(t, GetCtx(), gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, GetCtx(), gatewayNSN, clients)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying httpbin backend deployment")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(GetCtx(), deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing httpbin deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(GetCtx(), service, metav1.CreateOptions{})
	require.NoError(t, err)

	httpRoute := helpers.GenerateHTTPRoute(namespace.Name, gateway.Name, service.Name, func(h *gatewayv1.HTTPRoute) {
		h.Spec.Hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(host)}
	})

	t.Logf("creating httproute %s/%s to access deployment %s via kong", httpRoute.Namespace, httpRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := GetClients().GatewayClient.GatewayV1().HTTPRoutes(namespace.Name).Create(GetCtx(), httpRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy httproute %s/%s", httpRoute.Namespace, httpRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Log("verifying connectivity to the HTTPRoute")
	const (
		httpRouteAccessTimeout = 3 * time.Minute
		waitTick               = time.Second
	)

	httpClient := helpers.MustCreateHTTPClient(t, secret, host)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	request := helpers.MustBuildRequest(t, GetCtx(), http.MethodGet, "https://"+gatewayIPAddress+"/test", host)
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, clients, httpClient, request, "<title>httpbin.org</title>"),
		httpRouteAccessTimeout,
		time.Second,
	)
	t.Log("route to /test/1234 path of service httpbin should receive a 404 OK response")
	request = helpers.MustBuildRequest(t, GetCtx(), http.MethodGet, "https://"+gatewayIPAddress+"/test/1234", host)
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, clients, httpClient, request, "<h1>Not Found</h1>"),
		httpRouteAccessTimeout,
		time.Second,
	)
}
