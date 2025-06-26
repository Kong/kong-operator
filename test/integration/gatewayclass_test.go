package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/pkg/vars"
	"github.com/kong/kong-operator/test/helpers"
)

func TestGatewayClassUpdates(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying an unsupported GatewayClass resource")
	unsupportedGatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController("konghq.com/fake-operator"),
		},
	}
	unsupportedGatewayClass, err := GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), unsupportedGatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(unsupportedGatewayClass)

	t.Log("deploying a supported GatewayClass resource")
	gatewayClassName := uuid.NewString()
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	require.Eventually(t, testutils.GatewayClassIsAccepted(t, GetCtx(), gatewayClassName, clients),
		testutils.GatewayClassAcceptanceTimeLimit, time.Second)

	t.Log("deploying a Gateway using an unsupported class")
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(unsupportedGatewayClass.Name),
			Listeners: []gatewayv1.Listener{{
				Name:     "http",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(80),
			}},
		},
	}
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the controller doesn't try to schedule the unsupported Gateway")
	timeout := time.Now().Add(time.Second * 5)
	for timeout.After(time.Now()) {
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Get(GetCtx(), gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, gatewayutils.IsAccepted(gateway))
	}

	t.Log("updating unsupported Gateway to use the supported GatewayClass")
	require.Eventually(t, func() bool {
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Get(GetCtx(), gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		gateway.Spec.GatewayClassName = gatewayv1.ObjectName(gatewayClass.Name)
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Update(GetCtx(), gateway, metav1.UpdateOptions{})
		return err == nil
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("verifying that the updated Gateway is now considered supported and becomes scheduled")
	require.Eventually(t, func() bool {
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Get(GetCtx(), gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsAccepted(gateway)
	}, testutils.GatewaySchedulingTimeLimit, time.Second)
}

func TestGatewayClassCreation(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying a Gateway with a non-existent GatewayClass")
	gatewayClassName := uuid.NewString()
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClassName),
			Listeners: []gatewayv1.Listener{{
				Name:     "http",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(80),
			}},
		},
	}
	gateway, err := GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the controller doesn't try to schedule the unsupported Gateway")
	timeout := time.Now().Add(time.Second * 5)
	for timeout.After(time.Now()) {
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Get(GetCtx(), gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, gatewayutils.IsAccepted(gateway))
	}

	t.Log("creating a supported GatewayClass using the non-existent GatewayClass name")
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}

	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	require.Eventually(t, testutils.GatewayClassIsAccepted(t, GetCtx(), gatewayClassName, clients),
		testutils.GatewayClassAcceptanceTimeLimit, time.Second)

	t.Log("verifying that the Gateway is now considered supported and becomes scheduled")
	require.Eventually(t, func() bool {
		gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Get(GetCtx(), gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsAccepted(gateway)
	}, testutils.GatewaySchedulingTimeLimit, time.Second)
}
