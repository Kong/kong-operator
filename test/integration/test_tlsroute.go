package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/helpers/certificate"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestTLSRoute(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := GetClients().OperatorClient.ApisV1beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
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

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, func(gateway *gatewayv1.Gateway) {
		gateway.Spec.Listeners[0].Protocol = gatewayv1.TLSProtocolType
		gateway.Spec.Listeners[0].Port = gatewayv1.PortNumber(testutils.DefaultTLSListenerPort)
		gateway.Spec.Listeners[0].TLS = &gatewayv1.GatewayTLSConfig{
			Mode: lo.ToPtr(gatewayv1.TLSModePassthrough),
		}
	})
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

	const host = "tlsroute.integration.tests.org"
	cert, key := certificate.MustGenerateSelfSignedCertPEMFormat(certificate.WithDNSNames(host))

	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      strings.ReplaceAll(host, ".", "-"),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	t.Logf("deploying Secret %s/%s", tlsSecret.Namespace, tlsSecret.Name)
	tlsSecret, err = GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(GetCtx(), tlsSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Log("creating a tcpecho pod to test TLSRoute traffic routing")
	testUUID := uuid.NewString() // go-echo sends a "Running on Pod <UUID>." immediately on connecting
	deployment := generators.NewDeploymentForContainer(helpers.GenerateTLSEchoContainer(testutils.TCPEchoImage, testutils.TCPEchoTLSPort, testUUID, tlsSecret.Name))
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: tlsSecret.Name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: tlsSecret.Name,
			},
		},
	})
	deployment, err = GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(GetCtx(), deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(GetCtx(), service, metav1.CreateOptions{})
	require.NoError(t, err)

	tlsRoute := helpers.GenerateTLSRoute(namespace.Name, gateway.Name, service.Name, testutils.TCPEchoTLSPort, func(h *gatewayv1alpha2.TLSRoute) {
		h.Spec.Hostnames = []gatewayv1alpha2.Hostname{gatewayv1alpha2.Hostname(host)}
	})

	t.Logf("creating tlsroute %s/%s to access deployment %s via kong", tlsRoute.Namespace, tlsRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := GetClients().GatewayClient.GatewayV1alpha2().TLSRoutes(namespace.Name).Create(GetCtx(), tlsRoute, metav1.CreateOptions{})
			if err != nil {
				t.Logf("failed to deploy tlsroute: %v", err)
				c.Errorf("failed to deploy tlsroute: %v", err)
				return
			}
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Log("verifying that the tcpecho is responding properly over TLS")
	proxyTLSURL := fmt.Sprintf("%s:%d", gatewayIPAddress, testutils.DefaultTLSListenerPort)
	require.Eventually(t, func() bool {
		if err := helpers.TLSEchoResponds(proxyTLSURL, testUUID, host, tlsSecret, true); err != nil {
			t.Logf("failed accessing tcpecho at %s, err: %v", proxyTLSURL, err)
			return false
		}
		return true
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

}
