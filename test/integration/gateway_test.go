//go:build integration_tests
// +build integration_tests

package integration

import (
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
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kong/gateway-operator/internal/consts"
	"github.com/kong/gateway-operator/pkg/vars"
)

var (
	// gatewaySchedulingTimeLimit is the maximum amount of time to wait for
	// a supported Gateway to be marked as Scheduled by the gateway controller.
	gatewaySchedulingTimeLimit = time.Second * 7

	// gatewayReadyTimeLimit is the maximum amount of time to wait for a
	// supported Gateway to be fully provisioned and marked as Ready by the
	// gateway controller.
	gatewayReadyTimeLimit = time.Minute * 2
)

func TestGatewayEssentials(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	t.Log("deploying a GatewayClass resource")
	gatewayClass := generateGatewayClass()
	gatewayClass, err := gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := generateGateway(gatewayNSN, gatewayClass)
	gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, gatewayIsScheduled(t, ctx, gatewayNSN), gatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Ready")
	require.Eventually(t, gatewayIsReady(t, ctx, gatewayNSN), gatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	require.Eventually(t, gatewayIpAddressExist(t, ctx, gatewayNSN), subresourceReadinessWait, time.Second)
	gateway = mustGetGateway(t, ctx, gatewayNSN)
	gatewayIPAddress := gateway.Status.Addresses[0].Value

	t.Log("verifying that the DataPlane becomes provisioned")
	require.Eventually(t, gatewayDataPlaneIsProvisioned(t, gateway), subresourceReadinessWait, time.Second)
	dataplane := mustListDataPlanesForGateway(t, ctx, gateway)[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, gatewayControlPlaneIsProvisioned(t, gateway), subresourceReadinessWait, time.Second)
	controlplane := mustListControlPlanesForGateway(t, gateway)[0]

	t.Log("verifying networkpolicies are created")
	require.Eventually(t, gatewayNetworkPoliciesExist(t, ctx, gateway), subresourceReadinessWait, time.Second)

	t.Log("verifying connectivity to the Gateway")

	require.Eventually(t, getResponseBodyContains(t, ctx, "http://"+gatewayIPAddress, defaultKongResponseBody), subresourceReadinessWait, time.Second)

	t.Log("deleting Gateway resource")
	require.NoError(t, gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

	t.Log("verifying that DataPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := operatorClient.ApisV1alpha1().DataPlanes(namespace.Name).Get(ctx, dataplane.Name, metav1.GetOptions{})
		return errors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying that ControlPlane sub-resources are deleted")
	assert.Eventually(t, func() bool {
		_, err := operatorClient.ApisV1alpha1().ControlPlanes(namespace.Name).Get(ctx, controlplane.Name, metav1.GetOptions{})
		return errors.IsNotFound(err)
	}, time.Minute, time.Second)

	t.Log("verifying networkpolicies are deleted")
	require.Eventually(t, Not(gatewayNetworkPoliciesExist(t, ctx, gateway)), time.Minute, time.Second)
}

func TestGatewayDataPlaneNetworkPolicy(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	t.Log("deploying a GatewayClass resource")
	gatewayClass := generateGatewayClass()
	gatewayClass, err := gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}
	gateway := generateGateway(gatewayNSN, gatewayClass)
	gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, gatewayIsScheduled(t, ctx, gatewayNSN), gatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Ready")
	require.Eventually(t, gatewayIsReady(t, ctx, gatewayNSN), gatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the DataPlane becomes provisioned")
	require.Eventually(t, gatewayDataPlaneIsProvisioned(t, gateway), subresourceReadinessWait, time.Second)
	dataplane := mustListDataPlanesForGateway(t, ctx, gateway)[0]

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, gatewayControlPlaneIsProvisioned(t, gateway), subresourceReadinessWait, time.Second)
	controlplane := mustListControlPlanesForGateway(t, gateway)[0]

	t.Log("verifying DataPlane's NetworkPolicies is created")
	require.Eventually(t, gatewayNetworkPoliciesExist(t, ctx, gateway), subresourceReadinessWait, time.Second)
	networkpolicies := mustListGatewayNetworkPolicies(t, ctx, gateway)
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
		k8sClient.NetworkingV1().
			NetworkPolicies(networkPolicy.Namespace).
			Delete(ctx, networkPolicy.Name, metav1.DeleteOptions{}),
	)

	t.Log("verifying NetworkPolicies are recreated")
	require.Eventually(t, gatewayNetworkPoliciesExist(t, ctx, gateway), subresourceReadinessWait, time.Second)
	networkpolicies = mustListGatewayNetworkPolicies(t, ctx, gateway)
	require.Len(t, networkpolicies, 1)
	networkPolicy = networkpolicies[0]

	t.Log("verifying DataPlane's NetworkPolicies ingress rules correctness")
	require.Contains(t, networkPolicy.Spec.Ingress, expectLimitedAdminAPI.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowProxyIngress.Rule)
	require.Contains(t, networkPolicy.Spec.Ingress, expectAllowMetricsIngress.Rule)

	t.Log("deleting Gateway resource")
	require.NoError(t, gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))

	t.Log("verifying networkpolicies are deleted")
	require.Eventually(t, Not(gatewayNetworkPoliciesExist(t, ctx, gateway)), time.Minute, time.Second)
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

func generateGatewayClass() *gatewayv1alpha2.GatewayClass {
	gatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
		},
	}
	return gatewayClass
}

func generateGateway(gatewayNSN types.NamespacedName, gatewayClass *gatewayv1alpha2.GatewayClass) *gatewayv1alpha2.Gateway {
	gateway := &gatewayv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gatewayNSN.Namespace,
			Name:      gatewayNSN.Name,
		},
		Spec: gatewayv1alpha2.GatewaySpec{
			GatewayClassName: gatewayv1alpha2.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1alpha2.Listener{{
				Name:     "http",
				Protocol: gatewayv1alpha2.HTTPProtocolType,
				Port:     gatewayv1alpha2.PortNumber(80),
			}},
		},
	}
	return gateway
}
