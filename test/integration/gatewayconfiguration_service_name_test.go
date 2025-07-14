package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/pkg/vars"
	"github.com/kong/kong-operator/test/helpers"
)

func TestGatewayConfigurationServiceName(t *testing.T) {
	t.Skip("skipping as this test requires changed in the GatewayConfiguration API: https://github.com/kong/kong-operator/issues/1608")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Create a custom service name
	customServiceName := "custom-service-name-" + uuid.NewString()

	t.Log("deploying a GatewayConfiguration resource with a custom service name")
	gatewayConfig := &operatorv1beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
				Network: operatorv1beta1.GatewayConfigDataPlaneNetworkOptions{
					Services: &operatorv1beta1.GatewayConfigDataPlaneServices{
						Ingress: &operatorv1beta1.GatewayConfigServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Name: &customServiceName,
							},
						},
					},
				},
			},
		},
	}
	gatewayConfig, err := GetClients().OperatorClient.GatewayOperatorV1beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource with the GatewayConfiguration attached via ParametersReference")
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
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClass.Name),
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

	t.Log("verifying that the DataPlane has the custom service name")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		dp := dataplanes[0]
		if dp.Spec.Network.Services == nil || dp.Spec.Network.Services.Ingress == nil || dp.Spec.Network.Services.Ingress.Name == nil {
			return false
		}
		return *dp.Spec.Network.Services.Ingress.Name == customServiceName
	}, testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	t.Log("verifying that the service has the custom name using DataPlane's status.service field")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		dp := dataplanes[0]
		return dp.Status.Service == customServiceName
	}, testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)
}
