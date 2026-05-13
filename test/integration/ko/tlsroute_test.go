package integration

import (
	"crypto/x509"
	"fmt"
	"strconv"
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

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/modules/manager/config"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/integration"
)

const (
	tlsPort     = 1030
	tcpEchoPort = 1025
)

func setTLSListener(port int, mode gatewayv1.TLSModeType, certSecretName string) func(*gatewayv1.Gateway) {
	tlsConfig := &gatewayv1.ListenerTLSConfig{
		Mode: &mode,
	}
	if mode != gatewayv1.TLSModePassthrough {
		tlsConfig.CertificateRefs = []gatewayv1.SecretObjectReference{
			{
				Name: gatewayv1.ObjectName(certSecretName),
			},
		}
	}
	tlsListener := gatewayv1.Listener{
		Name:     "tls",
		Port:     gatewayv1.PortNumber(port),
		Protocol: gatewayv1.TLSProtocolType,
		TLS:      tlsConfig,
	}
	return func(gw *gatewayv1.Gateway) {

		for i, listener := range gw.Spec.Listeners {
			if listener.Name == "tls" {
				gw.Spec.Listeners[i] = tlsListener
				return
			}
		}

		gw.Spec.Listeners = append(gw.Spec.Listeners, tlsListener)
	}
}

func TestTLSRoutePassthrough(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	const host = "tlsroute.integration.tests.org"
	certSecretName := host + "-cert"
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithDNSNames(host))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      certSecretName,
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
	secret, err = integration.GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, setTLSListener(tlsPort, gatewayv1.TLSModePassthrough, secret.Name))
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Scheduled", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying tlsecho backend deployment")
	container := generators.NewContainer("tlsecho", testutils.TCPEchoImage, tlsPort)
	container.Env = []corev1.EnvVar{
		{
			Name:  "POD_NAME",
			Value: "test-tls-echo",
		},
		{
			Name:  "TLS_PORT",
			Value: strconv.Itoa(tlsPort),
		},
		{
			Name:  "TLS_CERT_FILE",
			Value: "/var/run/certs/tls.crt",
		},
		{
			Name:  "TLS_KEY_FILE",
			Value: "/var/run/certs/tls.key",
		},
	}
	container.VolumeMounts = []corev1.VolumeMount{
		{
			MountPath: "/var/run/certs",
			Name:      "cert-secret",
			ReadOnly:  true,
		},
	}
	deployment := generators.NewDeploymentForContainer(container)
	deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
		{
			Name: "cert-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  secret.Name,
					DefaultMode: new(int32(0o644)),
				},
			},
		},
	}
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing tlsecho deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("generating a TLSRoute")
	tlsRoute := helpers.GenerateTLSRoute(namespace.Name, gateway.Name, service.Name, tlsPort, func(r *gatewayv1.TLSRoute) {
		r.Spec.Hostnames = []gatewayv1.Hostname{
			gatewayv1.Hostname(host),
		}
	})

	t.Logf("creating tlsroute %s/%s to access deployment %s via kong", tlsRoute.Namespace, tlsRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := integration.GetClients().GatewayClient.GatewayV1().TLSRoutes(namespace.Name).Create(ctx, tlsRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy tlsroute %s/%s", tlsRoute.Namespace, tlsRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Logf("verifying connectivity to the TLSRoute")
	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(cert), "Should add certificate to cert pool successfully")
	require.Eventually(t, func() bool {
		err := helpers.EchoResponds(t, helpers.ProtocolTLS, fmt.Sprintf("%s:%d", gatewayIPAddress, tlsPort), "test-tls-echo",
			helpers.TLSOpt{
				Hostname:    host,
				CertPool:    certPool,
				Passthrough: true,
			})
		if err != nil {
			t.Logf("failed to access TLSRoute on %s:%d, error %+v", gatewayIPAddress, tlsPort, err)
			return false
		}
		return true
	}, testutils.DefaultTLSRouteWait, testutils.WaitTLSRouteTick)
}

func TestTLSTerminate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := integration.GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = integration.GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	const host = "tlsroute-terminate.integration.tests.org"
	certSecretName := host + "-cert"
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithDNSNames(host))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      certSecretName,
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
	secret, err = integration.GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(ctx, secret, metav1.CreateOptions{})
	require.NoError(t, err)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass, setTLSListener(tlsPort, gatewayv1.TLSModeTerminate, secret.Name))
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = integration.GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Scheduled", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsAccepted(t, ctx, gatewayNSN, clients), testutils.GatewaySchedulingTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients.MgrClient), testutils.GatewayReadyTimeLimit, time.Second)
	t.Logf("verifying Gateway %s/%s Listeners get marked as Programmed", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Logf("verifying Gateway %s/%s gets an IP address", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients.MgrClient)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("deploying tcpecho backend deployment")
	container := generators.NewContainer("tcpecho", testutils.TCPEchoImage, tcpEchoPort)
	container.Env = []corev1.EnvVar{
		{
			Name:  "POD_NAME",
			Value: "test-tls-terminate-echo",
		},
	}
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = integration.GetEnv().Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("exposing tcpecho deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	_, err = integration.GetEnv().Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)

	t.Logf("generating a TLSRoute")
	tlsRoute := helpers.GenerateTLSRoute(namespace.Name, gateway.Name, service.Name, tcpEchoPort, func(r *gatewayv1.TLSRoute) {
		r.Spec.Hostnames = []gatewayv1.Hostname{
			gatewayv1.Hostname(host),
		}
	})

	t.Logf("creating tlsroute %s/%s to access deployment %s via kong", tlsRoute.Namespace, tlsRoute.Name, deployment.Name)
	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			result, err := integration.GetClients().GatewayClient.GatewayV1().TLSRoutes(namespace.Name).Create(ctx, tlsRoute, metav1.CreateOptions{})
			require.NoError(c, err, "failed to deploy tlsroute %s/%s", tlsRoute.Namespace, tlsRoute.Name)
			cleaner.Add(result)
		},
		testutils.DefaultIngressWait, testutils.WaitIngressTick,
	)

	t.Logf("verifying connectivity to the TLSRoute")
	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(cert), "Should add certificate to cert pool successfully")
	require.Eventually(t, func() bool {
		err := helpers.EchoResponds(t, helpers.ProtocolTLS, fmt.Sprintf("%s:%d", gatewayIPAddress, tcpEchoPort), "test-tls-terminate-echo",
			helpers.TLSOpt{
				Hostname: host,
				CertPool: certPool,
			})
		if err != nil {
			t.Logf("failed to access TLSRoute on %s:%d, error %+v", gatewayIPAddress, tcpEchoPort, err)
			return false
		}
		return true
	}, testutils.DefaultTLSRouteWait, testutils.WaitTLSRouteTick)
}
