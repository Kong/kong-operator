package gateway

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Status Updates
// -----------------------------------------------------------------------------

// IsScheduled indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsScheduled(gateway *gwtypes.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1beta1.GatewayConditionAccepted) &&
			cond.Reason == string(gatewayv1beta1.GatewayClassReasonAccepted) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsReady indicates whether or not the provided Gateway object was
// marked as ready by the controller.
func IsReady(gateway *gwtypes.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1beta1.GatewayConditionReady) &&
			cond.Reason == string(kubernetes.ResourceReadyReason) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// AreListenersReady indicates whether or not all the provided Gateway listeners were
// marked as ready by the controller.
func AreListenersReady(gateway *gwtypes.Gateway) bool {
	return lo.ContainsBy(gateway.Spec.Listeners, func(listener gatewayv1beta1.Listener) bool {
		return lo.ContainsBy(gateway.Status.Listeners, func(listenerStatus gatewayv1beta1.ListenerStatus) bool {
			if listener.Name == listenerStatus.Name {
				return lo.ContainsBy(listenerStatus.Conditions, func(condition metav1.Condition) bool {
					return condition.Type == string(gatewayv1beta1.ListenerConditionReady) &&
						condition.Status == metav1.ConditionTrue
				})
			}
			return false
		})
	})
}
