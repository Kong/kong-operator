package gateway

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Status Updates
// -----------------------------------------------------------------------------

// IsScheduled indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsScheduled(gateway *gatewayv1beta1.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1beta1.GatewayConditionScheduled) &&
			cond.Reason == string(gatewayv1beta1.GatewayReasonScheduled) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsReady indicates whether or not the provided Gateway object was
// marked as ready by the controller.
func IsReady(gateway *gatewayv1beta1.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1beta1.GatewayConditionReady) &&
			cond.Reason == string(kubernetes.ResourceReadyReason) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
