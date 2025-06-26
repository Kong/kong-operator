package gatewayclass

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/pkg/vars"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
)

// -----------------------------------------------------------------------------
// GatewayClass - Decorator
// -----------------------------------------------------------------------------

// Decorator is a wrapper around upstream GatewayClass to add additional
// functionality.
type Decorator struct {
	*gatewayv1.GatewayClass
}

// NewDecorator returns Decorator object to add additional functionality to the base K8s GatewayClass.
func NewDecorator() *Decorator {
	return &Decorator{
		new(gatewayv1.GatewayClass),
	}
}

// GetConditions returns status conditions of GatewayClass.
func (gwc *Decorator) GetConditions() []metav1.Condition {
	return gwc.Status.Conditions
}

// SetConditions sets status conditions of GatewayClass.
func (gwc *Decorator) SetConditions(conditions []metav1.Condition) {
	gwc.Status.Conditions = conditions
}

// DecorateGatewayClass returns a Decorator object wrapping the provided
// GatewayClass object.
func DecorateGatewayClass(gwc *gatewayv1.GatewayClass) *Decorator {
	return &Decorator{GatewayClass: gwc}
}

// IsAccepted returns true if the GatewayClass has been accepted by the operator.
func (gwc *Decorator) IsAccepted() bool {
	if cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayClassConditionStatusAccepted), gwc); ok {
		return cond.Reason == string(gatewayv1.GatewayClassReasonAccepted) &&
			cond.ObservedGeneration == gwc.Generation && cond.Status == metav1.ConditionTrue
	}
	return false
}

// IsControlled returns boolean if the GatewayClass is controlled by this controller.
func (gwc *Decorator) IsControlled() bool {
	return string(gwc.Spec.ControllerName) == vars.ControllerName()
}
