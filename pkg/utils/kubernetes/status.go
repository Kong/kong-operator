package kubernetes

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	kcfgdataplane "github.com/kong/kong-operator/api/gateway-operator/dataplane"
)

// ConditionsAndListenerConditionsAndGenerationAware is a CRD type that has Conditions, Generation, and Listener
// Conditions.
type ConditionsAndListenerConditionsAndGenerationAware interface {
	ConditionsAndGenerationAware
	ListenersConditionsAware
}

// ConditionsAndGenerationAware represents a CRD type that has been enabled with metav1.Conditions,
// it can then benefit of a series of utility methods.
type ConditionsAndGenerationAware interface {
	GetGeneration() int64
	ConditionsAware
}

// ConditionsAware is a CRD that has Conditions.
type ConditionsAware interface {
	GetConditions() []metav1.Condition
	SetConditions(conditions []metav1.Condition)
}

// ListenersConditionsAware is a CRD that has Listener Conditions.
type ListenersConditionsAware interface {
	GetListenersConditions() []gatewayv1.ListenerStatus
	SetListenersConditions([]gatewayv1.ListenerStatus)
}

// SetCondition sets a new condition to the provided resource.
func SetCondition(condition metav1.Condition, resource ConditionsAware) {
	conditions := resource.GetConditions()
	newConditions := make([]metav1.Condition, 0, len(conditions))

	var conditionFound bool
	for i := range conditions {
		// NOTICE:
		// Skip "Scheduled" condition, it is condition type that was valid,
		// when ControlPlane was a separate deployment. It's taken into
		// account for compatibility reasons with v1beta1 (which sets it
		// by default). Only ControlPlane uses it, so it can be here.
		if conditions[i].Type == "Scheduled" {
			continue
		}
		if conditions[i].Type != condition.Type {
			newConditions = append(newConditions, conditions[i])
		} else {
			oldCondition := conditions[i]
			if conditionNeedsUpdate(oldCondition, condition) {
				newConditions = append(newConditions, condition)
			} else {
				newConditions = append(newConditions, oldCondition)
			}
			conditionFound = true
		}
	}
	if !conditionFound {
		newConditions = append(newConditions, condition)
	}
	resource.SetConditions(newConditions)
}

// GetCondition returns the condition with the given type, if it exists. If the condition does not exists it returns false.
func GetCondition(cType kcfgconsts.ConditionType, resource ConditionsAware) (metav1.Condition, bool) {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(cType) {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

// HasConditionWithStatus returns true if the provided resource has a condition
// with the given type and status.
func HasConditionWithStatus(cType kcfgconsts.ConditionType, resource ConditionsAware, status metav1.ConditionStatus) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(cType) {
			return condition.Status == status
		}
	}
	return false
}

// HasConditionFalse returns true if the condition on the resource has Status set to ConditionFalse, false otherwise.
func HasConditionFalse(cType kcfgconsts.ConditionType, resource ConditionsAware) bool {
	return HasConditionWithStatus(cType, resource, metav1.ConditionFalse)
}

// HasConditionTrue returns true if the condition on the resource has Status set to ConditionTrue, false otherwise.
func HasConditionTrue(cType kcfgconsts.ConditionType, resource ConditionsAware) bool {
	return HasConditionWithStatus(cType, resource, metav1.ConditionTrue)
}

// HasCondition returns true if the condition on the resource exists.
func HasCondition(cType kcfgconsts.ConditionType, resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(cType) {
			return true
		}
	}
	return false
}

// InitReady initializes the Ready status to False if Ready condition is not
// yet set on the resource.
func InitReady(resource ConditionsAndGenerationAware) bool {
	_, ok := GetCondition(kcfgdataplane.ReadyType, resource)
	if ok {
		return false
	}
	SetCondition(
		NewConditionWithGeneration(kcfgdataplane.ReadyType, metav1.ConditionFalse, kcfgdataplane.DependenciesNotReadyReason, kcfgdataplane.DependenciesNotReadyMessage, resource.GetGeneration()),
		resource,
	)
	return true
}

// SetReadyWithGeneration sets the Ready status to True if all the other conditions are True.
// It uses the provided generation to set the ObservedGeneration field.
func SetReadyWithGeneration(resource ConditionsAndGenerationAware, generation int64) {
	ready := metav1.Condition{
		Type:               string(kcfgdataplane.ReadyType),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: generation,
	}

	if AreAllConditionsHaveTrueStatus(resource) {
		ready.Status = metav1.ConditionTrue
		ready.Reason = string(kcfgdataplane.ResourceReadyReason)
	} else {
		ready.Status = metav1.ConditionFalse
		ready.Reason = string(kcfgdataplane.DependenciesNotReadyReason)
		ready.Message = kcfgdataplane.DependenciesNotReadyMessage
	}
	SetCondition(ready, resource)
}

// SetReady evaluates all the existing conditions and sets the Ready status accordingly.
func SetReady(resource ConditionsAndGenerationAware) {
	SetReadyWithGeneration(resource, resource.GetGeneration())
}

// SetProgrammed evaluates all the existing conditions and sets the Programmed status accordingly
func SetProgrammed(resource ConditionsAndGenerationAware) {
	programmed := metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: resource.GetGeneration(),
	}

	if AreAllConditionsHaveTrueStatus(resource) {
		programmed.Status = metav1.ConditionTrue
		programmed.Reason = string(gatewayv1.GatewayReasonProgrammed)
	} else {
		programmed.Status = metav1.ConditionFalse
		programmed.Reason = string(kcfgdataplane.DependenciesNotReadyReason)
		programmed.Message = kcfgdataplane.DependenciesNotReadyMessage
	}
	SetCondition(programmed, resource)
}

// SetAcceptedConditionOnGateway sets the gateway Accepted condition according to the Gateway API specification.
func SetAcceptedConditionOnGateway(resource ConditionsAndListenerConditionsAndGenerationAware) {
	oldCondition, NewCondition := metav1.Condition{}, metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonAccepted),
		ObservedGeneration: resource.GetGeneration(),
		LastTransitionTime: metav1.Now(),
	}

	// If even a single listener is not accepted or is conflicted, the gateway needs
	// to be marked as not accepted.
	for i, listStatus := range resource.GetListenersConditions() {
		for _, listCond := range listStatus.Conditions {
			if listCond.Type == string(gatewayv1.GatewayConditionAccepted) {
				if listCond.Status == metav1.ConditionFalse {
					if NewCondition.Message != "" {
						NewCondition.Message = fmt.Sprintf("%s ", NewCondition.Message)
					}
					NewCondition.Status = metav1.ConditionFalse
					NewCondition.Reason = string(gatewayv1.GatewayReasonListenersNotValid)
					NewCondition.Message = fmt.Sprintf("%sListener %d is not accepted.", NewCondition.Message, i)
				}
			}
			if listCond.Type == string(gatewayv1.ListenerConditionConflicted) {
				if listCond.Status == metav1.ConditionTrue {
					if NewCondition.Message != "" {
						NewCondition.Message = fmt.Sprintf("%s ", NewCondition.Message)
					}
					NewCondition.Status = metav1.ConditionFalse
					NewCondition.Reason = string(gatewayv1.GatewayReasonListenersNotValid)
					NewCondition.Message = fmt.Sprintf("%sListener %d is conflicted.", NewCondition.Message, i)
				}
			}
		}
	}
	if NewCondition.Message == "" {
		NewCondition.Message = "All listeners are accepted."
	}

	if NewCondition.Status != oldCondition.Status ||
		NewCondition.Reason != oldCondition.Reason {
		SetCondition(NewCondition, resource)
	}
}

// AreAllConditionsHaveTrueStatus checks if all the conditions on the resource are in the True state.
// It skips the Programmed condition as that particular condition will be set based on
// the return value of this function.
func AreAllConditionsHaveTrueStatus(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		switch condition.Type {
		case string(kcfgdataplane.ReadyType), string(gatewayv1.GatewayConditionProgrammed):
			continue
		default:
			if condition.Status != metav1.ConditionTrue {
				return false
			}
		}
	}
	return true
}

// IsReady evaluates whether a resource is in Ready state, meaning
// that all its conditions are in the True state.
func IsReady(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(kcfgdataplane.ReadyType) {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

// IsProgrammed evaluates whether a resource is in Programmed state.
func IsProgrammed(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(gatewayv1.GatewayConditionProgrammed) {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

// NewCondition convenience method for creating conditions
func NewCondition(cType kcfgconsts.ConditionType, status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               string(cType),
		Reason:             string(reason),
		Message:            message,
		LastTransitionTime: metav1.Now(),
		Status:             status,
	}
}

// NewConditionWithGeneration convenience method for creating conditions with ObservedGeneration set.
func NewConditionWithGeneration(cType kcfgconsts.ConditionType, status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string, observedGeneration int64) metav1.Condition {
	c := NewCondition(cType, status, reason, message)
	c.ObservedGeneration = observedGeneration
	return c
}

// ConditionsNeedsUpdate retrieves the persisted state and compares all the conditions
// to decide whether the status must be updated or not.
func ConditionsNeedsUpdate(current, updated ConditionsAware) bool {
	var (
		currentConditions = current.GetConditions()
		updatedConditions = updated.GetConditions()
	)

	if len(currentConditions) != len(updatedConditions) {
		return true
	}

	for _, c := range currentConditions {
		u, exists := GetCondition(kcfgconsts.ConditionType(c.Type), updated)
		if !exists {
			return true
		}
		if conditionNeedsUpdate(c, u) {
			return true
		}
	}
	return false
}

func conditionNeedsUpdate(current, updated metav1.Condition) bool {
	return updated.Reason != current.Reason || updated.Message != current.Message || updated.Status != current.Status || updated.ObservedGeneration != current.ObservedGeneration
}
