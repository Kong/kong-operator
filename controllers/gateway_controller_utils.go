package controllers

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Gateway - Errors
// -----------------------------------------------------------------------------

// ErrUnsupportedGateway is an error which indicates that a provided Gateway
// is not supported because it's GatewayClass was not associated with this
// controller.
var ErrUnsupportedGateway = fmt.Errorf("gateway not supported")

// -----------------------------------------------------------------------------
// Gateway - Private Functions - Status Updates
// -----------------------------------------------------------------------------

// maxConds is the maximum number of conditions that can be stored at one in a
// Gateway object.
const maxConds = 8

func pruneGatewayStatusConds(gateway *gatewayv1alpha2.Gateway) *gatewayv1alpha2.Gateway {
	if len(gateway.Status.Conditions) >= maxConds {
		gateway.Status.Conditions = gateway.Status.Conditions[len(gateway.Status.Conditions)-(maxConds-1):]
	}
	return gateway
}

// -----------------------------------------------------------------------------
// Private Functions - Gateway Object Labels
// -----------------------------------------------------------------------------

func labelObjForGateway(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.GatewayManagedLabelValue
	obj.SetLabels(labels)
}
