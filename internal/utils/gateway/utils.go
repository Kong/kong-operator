package gateway

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Public Functions - Status Updates
// -----------------------------------------------------------------------------

// IsGatewayScheduled indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsGatewayScheduled(gateway *gatewayv1alpha2.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1alpha2.GatewayConditionScheduled) &&
			cond.Reason == string(gatewayv1alpha2.GatewayReasonScheduled) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsGatewayScheduled indicates whether or not the provided Gateway object was
// marked as scheduled by the controller.
func IsGatewayReady(gateway *gatewayv1alpha2.Gateway) bool {
	for _, cond := range gateway.Status.Conditions {
		if cond.Type == string(gatewayv1alpha2.GatewayConditionReady) &&
			cond.Reason == string(gatewayv1alpha2.GatewayReasonReady) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
