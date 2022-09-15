package controllers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// -----------------------------------------------------------------------------
// GatewayClass Controller - Private
// -----------------------------------------------------------------------------

const maxConds = 8

// pruneGatewayClassStatusConds cleans out old status conditions if the
// Gatewayclass currently has more status conditions set than the 8 maximum
// allowed by the Kubernetes API.
func pruneGatewayClassStatusConds(gwc *gatewayv1beta1.GatewayClass) *gatewayv1beta1.GatewayClass {
	if len(gwc.Status.Conditions) > maxConds {
		gwc.Status.Conditions = gwc.Status.Conditions[len(gwc.Status.Conditions)-maxConds:]
	}
	return gwc
}

// setGatewayClassCondition sets the condition with specified type in gatewayclass status
// to expected condition in newCondition.
// if the gatewayclass status does not contain a condition with that type, add one more condition.
// if the gatewayclass status contains condition(s) with the type, then replace with the new condition.
func setGatewayClassCondition(gwc *gatewayv1beta1.GatewayClass, newCondition metav1.Condition) {
	newConditions := []metav1.Condition{}
	for _, condition := range gwc.Status.Conditions {
		if condition.Type != newCondition.Type {
			newConditions = append(newConditions, condition)
		}
	}
	newConditions = append(newConditions, newCondition)
	gwc.Status.Conditions = newConditions
}
