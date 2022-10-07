package test

import (
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/pkg/vars"
)

// GenerateGatewayClass generate the default GatewayClass to be used in tests
func GenerateGatewayClass() *gatewayv1beta1.GatewayClass {
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName),
		},
	}
	return gatewayClass
}

// GenerateGateway generate a Gateway to be used in tests
func GenerateGateway(gatewayNSN types.NamespacedName, gatewayClass *gatewayv1beta1.GatewayClass) *gwtypes.Gateway {
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gatewayNSN.Namespace,
			Name:      gatewayNSN.Name,
		},
		Spec: gatewayv1beta1.GatewaySpec{
			GatewayClassName: gatewayv1beta1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1beta1.Listener{{
				Name:     "http",
				Protocol: gatewayv1beta1.HTTPProtocolType,
				Port:     gatewayv1beta1.PortNumber(80),
			}},
		},
	}
	return gateway
}
