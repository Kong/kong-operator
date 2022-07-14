//go:build integration_tests
// +build integration_tests

package integration

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
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
	gatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
		},
	}
	gatewayClass, err := gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gatewayv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
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
	gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets marked as Scheduled")
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsGatewayScheduled(gateway)
	}, gatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying Gateway gets marked as Ready")
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsGatewayReady(gateway)
	}, gatewayReadyTimeLimit, time.Second)

	t.Log("verifying Gateway gets an IP address")
	var gatewayIP string
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(gateway.Status.Addresses) > 0 && *gateway.Status.Addresses[0].Type == gatewayv1alpha2.IPAddressType {
			gatewayIP = gateway.Status.Addresses[0].Value
			return true
		}
		return false
	}, subresourceReadinessWait, time.Second)

	t.Log("verifying that the DataPlane becomes provisioned")
	var dataplane *operatorv1alpha1.DataPlane
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) == 1 {
			for _, condition := range dataplanes[0].Status.Conditions {
				if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
					dataplane = &dataplanes[0]
					return true
				}
			}
		}
		return false
	}, subresourceReadinessWait, time.Second)
	require.NotNil(t, dataplane)

	t.Log("verifying that the ControlPlane becomes provisioned")
	var controlplane *operatorv1alpha1.ControlPlane
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) == 1 {
			for _, condition := range controlplanes[0].Status.Conditions {
				if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
					controlplane = &controlplanes[0]
					return true
				}
			}
		}
		return false
	}, subresourceReadinessWait, time.Second)
	require.NotNil(t, controlplane)

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, func() bool {
		resp, err := httpc.Get("http://" + gatewayIP)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return strings.Contains(string(body), defaultKongResponseBody)
	}, subresourceReadinessWait, time.Second)

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
}
