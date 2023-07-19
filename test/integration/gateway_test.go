//go:build integration_tests

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestGatewayEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := testutils.GenerateGatewayClass()
	gatewayClass, err := clients.GatewayClient.GatewayV1beta1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
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

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreReady(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("verifying that the DataPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayDataPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplane := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplane := testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying networkpolicies are created")
	require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress), testutils.SubresourceReadinessWait, time.Second)

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	dataplaneNN := types.NamespacedName{Namespace: namespace.Name, Name: dataplane.Name}
	controlplaneClient := clients.OperatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)
	controlplaneNN := types.NamespacedName{Namespace: namespace.Name, Name: controlplane.Name}

	t.Log("verifying that dataplane has 1 ready replica")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, ctx, dataplaneNN, clients, 1), time.Minute, time.Second)

	t.Log("verifying that controlplane has 1 ready replica")
	require.Eventually(t, testutils.ControlPlaneHasNReadyPods(t, ctx, controlplaneNN, clients, 1), time.Minute, time.Second)

	t.Log("deleting controlplane")
	require.NoError(t, controlplaneClient.Delete(ctx, controlplane.Name, metav1.DeleteOptions{}))

	t.Log("deleting dataplane")
	require.NoError(t, dataplaneClient.Delete(ctx, dataplane.Name, metav1.DeleteOptions{}))

	t.Log("verifying Gateway gets marked as not Programmed")
	require.Eventually(t, testutils.Not(testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients)), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreReady(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the ControlPlane becomes provisioned again")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), 45*time.Second, time.Second)
	controlplane = testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying that the DataPlane becomes provisioned again")
	require.Eventually(t, testutils.GatewayDataPlaneIsProvisioned(t, ctx, gateway, clients), 45*time.Second, time.Second)
	dataplane = testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying Gateway gets marked as Programmed again")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreReady(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address again")
	require.Eventually(t, testutils.GatewayIPAddressExist(t, ctx, gatewayNSN, clients), testutils.SubresourceReadinessWait, time.Second)
	gateway = testutils.MustGetGateway(t, ctx, gatewayNSN, clients)
	gatewayIPAddress = gateway.Status.Addresses[0].Value

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, expect404WithNoRouteFunc(t, ctx, "http://"+gatewayIPAddress), testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	dataplaneName := types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      dataplane.Name,
	}
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients), time.Minute, time.Second)

	t.Log("deleting the dataplane service")
	require.NoError(t, clients.MgrClient.Delete(ctx, &dataplaneService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneService, clients), time.Minute, time.Second)
	services := testutils.MustListDataPlaneProxyServices(t, ctx, &dataplane, clients.MgrClient)
	require.Len(t, services, 1)
	service := services[0]

	t.Log("verifying controlplane deployment updated with new dataplane service")
	require.Eventually(t, func() bool {
		controlDeployment := testutils.MustListControlPlaneDeployments(t, ctx, &controlplane, clients)[0]
		container := k8sutils.GetPodContainerByName(&controlDeployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
		if container == nil {
			return false
		}
		for _, envvar := range container.Env {
			if envvar.Name == "CONTROLLER_PUBLISH_SERVICE" {
				return envvar.Value == fmt.Sprintf("%s/%s", service.Namespace, service.Name)
			}
		}
		return false
	}, time.Minute*2, time.Second)

	t.Log("deleting Gateway resource")
	require.NoError(t, clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

	t.Log("verifying that DataPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name).Get(ctx, dataplane.Name, metav1.GetOptions{})
		return errors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that ControlPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := clients.OperatorClient.ApisV1alpha1().ControlPlanes(namespace.Name).Get(ctx, controlplane.Name, metav1.GetOptions{})
		return errors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying networkpolicies are deleted")
	require.Eventually(t, testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients)), time.Minute, time.Second)

	t.Log("verifying that gateway itself is deleted")
	require.Eventually(t, testutils.GatewayNotExist(t, ctx, gatewayNSN, clients), time.Minute, time.Second)
}

func TestGatewayDataPlaneNetworkPolicy(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	var err error
	gatewayConfigurationName := uuid.NewString()
	t.Log("deploying a GatewayConfiguration resource")
	gatewayConfiguration := testutils.GenerateGatewayConfiguration(types.NamespacedName{Namespace: namespace.Name, Name: gatewayConfigurationName})
	gatewayConfiguration, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfiguration, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfiguration)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := testutils.GenerateGatewayClass()
	gatewayClass.Spec.ParametersRef = &gatewayv1beta1.ParametersReference{
		Group:     "gateway-operator.konghq.com",
		Kind:      "GatewayConfiguration",
		Name:      gatewayConfigurationName,
		Namespace: (*gatewayv1beta1.Namespace)(&namespace.Name),
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

	t.Log("verifying Gateway gets marked as Programmed")
	require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)
	require.Eventually(t, testutils.GatewayListenersAreReady(t, ctx, gatewayNSN, clients), testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the DataPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayDataPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	dataplane := testutils.MustListDataPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, testutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	controlplane := testutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)[0]

	t.Log("verifying DataPlane's NetworkPolicies is created")
	require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	networkpolicies := testutils.MustListNetworkPoliciesForGateway(t, ctx, gateway, clients)
	require.Len(t, networkpolicies, 1)
	networkPolicy := networkpolicies[0]
	require.Equal(t, map[string]string{"app": dataplane.Name}, networkPolicy.Spec.PodSelector.MatchLabels)

	t.Log("verifying that the DataPlane's Pod Admin API is network restricted to ControlPlane Pods")
	var expectLimitedAdminAPI networkPolicyIngressRuleDecorator
	expectLimitedAdminAPI.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneAdminAPIPort)
	expectLimitedAdminAPI.withPeerMatchLabels(
		map[string]string{"app": controlplane.Name},
		map[string]string{"kubernetes.io/metadata.name": dataplane.Namespace},
	)

	t.Log("verifying that the DataPlane's proxy ingress traffic is allowed")
	var expectAllowProxyIngress networkPolicyIngressRuleDecorator
	expectAllowProxyIngress.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneProxyPort)
	expectAllowProxyIngress.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneProxySSLPort)

	t.Log("verifying that the DataPlane's metrics ingress traffic is allowed")
	var expectAllowMetricsIngress networkPolicyIngressRuleDecorator
	expectAllowMetricsIngress.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneMetricsPort)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules correctness")
	require.Contains(t, networkPolicy.Spec.Ingress, expectLimitedAdminAPI.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowProxyIngress.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowMetricsIngress.Rule)

	t.Log("deleting DataPlane's NetworkPolicies")
	require.NoError(t,
		clients.K8sClient.NetworkingV1().
			NetworkPolicies(networkPolicy.Namespace).
			Delete(ctx, networkPolicy.Name, metav1.DeleteOptions{}),
	)

	t.Log("verifying NetworkPolicies are recreated")
	require.Eventually(t, testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients), testutils.SubresourceReadinessWait, time.Second)
	networkpolicies = testutils.MustListNetworkPoliciesForGateway(t, ctx, gateway, clients)
	require.Len(t, networkpolicies, 1)
	networkPolicy = networkpolicies[0]
	t.Logf("NetworkPolicy generation %d", networkPolicy.Generation)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules correctness")
	require.Contains(t, networkPolicy.Spec.Ingress, expectLimitedAdminAPI.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowProxyIngress.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowMetricsIngress.Rule)

	t.Log("verifying DataPlane's NetworkPolicies get updated after customizing kong proxy listen port through GatewayConfiguration")
	setGatewayConfigurationEnvProxyPort(t, gatewayConfiguration, 8005, 8999)
	gatewayConfiguration, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Update(ctx, gatewayConfiguration, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules get updated with configured proxy listen port")
	var expectedUpdatedProxyListenPort networkPolicyIngressRuleDecorator
	expectedUpdatedProxyListenPort.withProtocolPort(corev1.ProtocolTCP, 8005)
	expectedUpdatedProxyListenPort.withProtocolPort(corev1.ProtocolTCP, 8999)
	require.Eventually(t,
		testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, expectedUpdatedProxyListenPort.Rule),
		testutils.SubresourceReadinessWait, time.Second)
	var notExpectedUpdatedProxyListenPort networkPolicyIngressRuleDecorator
	notExpectedUpdatedProxyListenPort.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneProxyPort)
	require.Eventually(t,
		testutils.Not(
			testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, notExpectedUpdatedProxyListenPort.Rule),
		),
		testutils.SubresourceReadinessWait, time.Second)

	t.Log("verifying DataPlane's NetworkPolicies ingress rules get updated with configured admin listen port")
	setGatewayConfigurationEnvAdminAPIPort(t, gatewayConfiguration, 8555)
	_, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Update(ctx, gatewayConfiguration, metav1.UpdateOptions{})
	require.NoError(t, err)
	var expectedUpdatedLimitedAdminAPI networkPolicyIngressRuleDecorator
	expectedUpdatedLimitedAdminAPI.withProtocolPort(corev1.ProtocolTCP, 8555)
	expectedUpdatedLimitedAdminAPI.withPeerMatchLabels(
		map[string]string{"app": controlplane.Name},
		map[string]string{"kubernetes.io/metadata.name": controlplane.Namespace},
	)
	require.Eventually(t,
		testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, expectedUpdatedLimitedAdminAPI.Rule),
		testutils.SubresourceReadinessWait, time.Second)
	var notExpectedUpdatedLimitedAdminAPI networkPolicyIngressRuleDecorator
	notExpectedUpdatedLimitedAdminAPI.withProtocolPort(corev1.ProtocolTCP, consts.DataPlaneAdminAPIPort)
	notExpectedUpdatedLimitedAdminAPI.withPeerMatchLabels(
		map[string]string{"app": controlplane.Name},
		map[string]string{"kubernetes.io/metadata.name": controlplane.Namespace},
	)
	require.Eventually(t,
		testutils.Not(testutils.GatewayNetworkPolicyForGatewayContainsRules(t, ctx, gateway, clients, notExpectedUpdatedLimitedAdminAPI.Rule)),
		testutils.SubresourceReadinessWait, time.Second)

	t.Log("deleting Gateway resource")
	require.NoError(t, clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

	t.Log("verifying networkpolicies are deleted")
	require.Eventually(t, testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, gateway, clients)), time.Minute, time.Second)
}

func setGatewayConfigurationEnvProxyPort(t *testing.T, gatewayConfiguration *operatorv1alpha1.GatewayConfiguration, proxyPort int, proxySSLPort int) {
	t.Helper()

	dpOptions := gatewayConfiguration.Spec.DataPlaneOptions
	if dpOptions == nil {
		dpOptions = &operatorv1alpha1.DataPlaneOptions{}
	}
	if dpOptions.Deployment.PodTemplateSpec == nil {
		dpOptions.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	container := k8sutils.GetPodContainerByName(&dpOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)

	container.Env = setEnvValueByName(container.Env,
		"KONG_PROXY_LISTEN",
		fmt.Sprintf("0.0.0.0:%d reuseport backlog=16384, 0.0.0.0:%d http2 ssl reuseport backlog=16384", proxyPort, proxySSLPort),
	)
	container.Env = setEnvValueByName(container.Env,
		"KONG_PORT_MAPS",
		fmt.Sprintf("80:%d, 443:%d", proxyPort, proxySSLPort),
	)

	gatewayConfiguration.Spec.DataPlaneOptions = dpOptions
}

func setGatewayConfigurationEnvAdminAPIPort(t *testing.T, gatewayConfiguration *operatorv1alpha1.GatewayConfiguration, adminAPIPort int) {
	t.Helper()

	dpOptions := gatewayConfiguration.Spec.DataPlaneOptions
	if dpOptions == nil {
		dpOptions = &operatorv1alpha1.DataPlaneOptions{}
	}

	container := k8sutils.GetPodContainerByName(&dpOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)

	container.Env = setEnvValueByName(container.Env,
		"KONG_ADMIN_LISTEN",
		fmt.Sprintf("0.0.0.0:%d ssl reuseport backlog=16384", adminAPIPort),
	)

	gatewayConfiguration.Spec.DataPlaneOptions = dpOptions
}

type networkPolicyIngressRuleDecorator struct {
	Rule networkingv1.NetworkPolicyIngressRule
}

func (d *networkPolicyIngressRuleDecorator) withProtocolPort(protocol corev1.Protocol, port int) { //nolint:unparam
	portIntStr := intstr.FromInt(port)
	d.Rule.Ports = append(d.Rule.Ports, networkingv1.NetworkPolicyPort{
		Protocol: &protocol,
		Port:     &portIntStr,
	})
}

func (d *networkPolicyIngressRuleDecorator) withPeerMatchLabels(
	podSelector map[string]string,
	namespaceSelector map[string]string,
) {
	d.Rule.From = append(d.Rule.From, networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: podSelector,
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: namespaceSelector,
		},
	})
}
