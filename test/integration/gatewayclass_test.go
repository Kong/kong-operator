//go:build integration_tests
// +build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	"github.com/kong/gateway-operator/pkg/vars"
)

func TestGatewayClassUpdates(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	t.Log("deploying an unsupported GatewayClass resource")
	unsupportedGatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ControllerName: gatewayv1alpha2.GatewayController("konghq.com/fake-operator"),
		},
	}
	unsupportedGatewayClass, err := gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, unsupportedGatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(unsupportedGatewayClass)

	t.Log("deploying a supported GatewayClass resource")
	gatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
		},
	}
	gatewayClass, err = gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying a Gateway using an unsupported class")
	gateway := &gatewayv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewaySpec{
			GatewayClassName: gatewayv1alpha2.ObjectName(unsupportedGatewayClass.Name),
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

	t.Log("verifying that the controller doesn't try to schedule the unsupported Gateway")
	timeout := time.Now().Add(time.Second * 5)
	for timeout.After(time.Now()) {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, gatewayutils.IsGatewayScheduled(gateway))
	}

	t.Log("updating unsupported Gateway to use the supported GatewayClass")
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		gateway.Spec.GatewayClassName = gatewayv1alpha2.ObjectName(gatewayClass.Name)
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Update(ctx, gateway, metav1.UpdateOptions{})
		return err == nil
	}, objectUpdateTimeout, time.Second)

	t.Log("verifying that the updated Gateway is now considered supported and becomes scheduled")
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsGatewayScheduled(gateway)
	}, gatewaySchedulingTimeLimit, time.Second)
}

func TestGatewayClassCreation(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	t.Log("deploying a Gateway with a non-existent GatewayClass")
	gatewayClassName := uuid.NewString()
	gateway := &gatewayv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewaySpec{
			GatewayClassName: gatewayv1alpha2.ObjectName(gatewayClassName),
			Listeners: []gatewayv1alpha2.Listener{{
				Name:     "http",
				Protocol: gatewayv1alpha2.HTTPProtocolType,
				Port:     gatewayv1alpha2.PortNumber(80),
			}},
		},
	}
	gateway, err := gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the controller doesn't try to schedule the unsupported Gateway")
	timeout := time.Now().Add(time.Second * 5)
	for timeout.After(time.Now()) {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, gatewayutils.IsGatewayScheduled(gateway))
	}

	t.Log("creating a supported GatewayClass using the non-existent GatewayClass name")
	gatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
		},
	}
	gatewayClass, err = gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("verifying that the Gateway is now considered supported and becomes scheduled")
	require.Eventually(t, func() bool {
		gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return gatewayutils.IsGatewayScheduled(gateway)
	}, gatewaySchedulingTimeLimit, time.Second)
}
