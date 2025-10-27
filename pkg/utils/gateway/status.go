package gateway

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Status Updates
// -----------------------------------------------------------------------------

// IsAccepted indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsAccepted(gateway *gwtypes.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1.GatewayConditionAccepted) &&
			cond.Reason == string(gatewayv1.GatewayClassReasonAccepted) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsProgrammed indicates whether or not the provided Gateway object was
// marked as Programmed by the controller.
func IsProgrammed(gateway *gwtypes.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1.GatewayConditionProgrammed) &&
			cond.Reason == string(gatewayv1.GatewayReasonProgrammed) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// AreListenersProgrammed indicates whether or not all the provided Gateway
// listeners were marked as Programmed by the controller.
func AreListenersProgrammed(gateway *gwtypes.Gateway) bool {
	return lo.EveryBy(gateway.Spec.Listeners, func(listener gwtypes.Listener) bool {
		return IsListenerProgrammed(gateway, listener.Name)
	})
}

// IsListenerProgrammed returns true if the listener with given name
// has the "Programmed" condition and set to True in the given gateway.
func IsListenerProgrammed(gateway *gwtypes.Gateway, listenerName gwtypes.SectionName) bool {
	return lo.ContainsBy(gateway.Status.Listeners, func(listenerStatus gatewayv1.ListenerStatus) bool {
		// Return false if no status with the given name in gateway.status.listeners.
		if listenerName != listenerStatus.Name {
			return false
		}
		// Find the "Programmed" condition inside the listener status if name matches.
		return lo.ContainsBy(listenerStatus.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == string(gatewayv1.ListenerConditionProgrammed) &&
				condition.Status == metav1.ConditionTrue
		})
	})
}
