package gateway

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// -----------------------------------------------------------------------------
// Gateway Utils - Status Updates
// -----------------------------------------------------------------------------

// MaxStatusConditions is the maximum number of conditions that can be stored at one in a
// Gateway object.
const MaxStatusConditions = 8

// PruneGatewayStatusConds ensures that the status conditions for the provided
// Gateway are not over the maximum allowed number.
func PruneGatewayStatusConds(gateway *gatewayv1alpha2.Gateway) *gatewayv1alpha2.Gateway {
	if len(gateway.Status.Conditions) >= MaxStatusConditions {
		gateway.Status.Conditions = gateway.Status.Conditions[len(gateway.Status.Conditions)-(MaxStatusConditions-1):]
	}
	return gateway
}

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

// IsGatewayReady indicates whether or not the provided Gateway object was
// marked as ready by the controller.
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
