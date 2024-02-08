package kubernetes

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ConditionType literal that defines the different types of condition
type ConditionType string

// CoditionReason literal to enumerate a specific condition reason
type ConditionReason string

const (
	// ReadyType indicates if the resource has all the dependent conditions Ready
	ReadyType ConditionType = "Ready"

	// DependenciesNotReadyReason is a generic reason describing that the other Conditions are not true
	DependenciesNotReadyReason ConditionReason = "DependenciesNotReady"

	// ResourceReadyReason indicates the resource is ready
	ResourceReadyReason ConditionReason = ConditionReason("Ready")

	// WaitingToBecomeReadyReason generic message for dependent resources waiting to be ready
	WaitingToBecomeReadyReason ConditionReason = "WaitingToBecomeReady"

	// ResourceCreatedOrUpdatedReason generic message for missing or outdated resources
	ResourceCreatedOrUpdatedReason ConditionReason = "ResourceCreatedOrUpdated"

	// UnableToProvisionReason generic message for unexpected errors
	UnableToProvisionReason ConditionReason = "UnableToProvision"

	// DependenciesNotReadyMessage indicates the other conditions are not yet ready
	DependenciesNotReadyMessage = "There are other conditions that are not yet ready"

	// WaitingToBecomeReadyMessage indicates the target resource is not ready
	WaitingToBecomeReadyMessage = "Waiting for the resource to become ready"

	// ResourceCreatedMessage indicates a missing resource was provisioned
	ResourceCreatedMessage = "Resource has been created"

	// ResourceUpdatedMessage indicates a resource was updated
	ResourceUpdatedMessage = "Resource has been updated"
)

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

type ConditionsAware interface {
	GetConditions() []metav1.Condition
	SetConditions(conditions []metav1.Condition)
}

type ListenersConditionsAware interface {
	GetListenersConditions() []gatewayv1.ListenerStatus
	SetListenersConditions([]gatewayv1.ListenerStatus)
}

// SetCondition sets a new condition to the provided resource
func SetCondition(condition metav1.Condition, resource ConditionsAware) {
	conditions := resource.GetConditions()
	newConditions := make([]metav1.Condition, 0, len(conditions))

	for i := 0; i < len(conditions); i++ {
		if conditions[i].Type != condition.Type {
			newConditions = append(newConditions, conditions[i])
		}
	}

	newConditions = append(newConditions, condition)
	resource.SetConditions(newConditions)
}

// GetCondition returns the condition with the given type, if it exists. If the condition does not exists it returns false.
func GetCondition(cType ConditionType, resource ConditionsAware) (metav1.Condition, bool) {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(cType) {
			return condition, true
		}
	}
	return metav1.Condition{}, false
}

// IsValidCondition returns a true value whether the condition is ConditionTrue, false otherwise
func IsValidCondition(cType ConditionType, resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(cType) {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

// InitReady initializes the Ready status to False if Ready condition is not
// yet set on the resource.
func InitReady(resource ConditionsAndGenerationAware) {
	_, ok := GetCondition(ReadyType, resource)
	if ok {
		return
	}
	SetCondition(
		NewConditionWithGeneration(ReadyType, metav1.ConditionFalse, DependenciesNotReadyReason, DependenciesNotReadyMessage, resource.GetGeneration()),
		resource,
	)
}

// InitProgrammed initializes the Programmed status to False
func InitProgrammed(resource ConditionsAware) {
	SetCondition(NewCondition(ConditionType(gatewayv1.GatewayConditionProgrammed), metav1.ConditionFalse, ConditionReason(gatewayv1.GatewayReasonPending), DependenciesNotReadyMessage), resource)
}

// SetReadyWithGeneration sets the Ready status to True if all the other conditions are True.
// It uses the provided generation to set the ObservedGeneration field.
func SetReadyWithGeneration(resource ConditionsAndGenerationAware, generation int64) {
	ready := metav1.Condition{
		Type:               string(ReadyType),
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: generation,
	}

	if areAllConditionsHaveTrueStatus(resource) {
		ready.Status = metav1.ConditionTrue
		ready.Reason = string(ResourceReadyReason)
	} else {
		ready.Status = metav1.ConditionFalse
		ready.Reason = string(DependenciesNotReadyReason)
		ready.Message = DependenciesNotReadyMessage
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

	if areAllConditionsHaveTrueStatus(resource) {
		programmed.Status = metav1.ConditionTrue
		programmed.Reason = string(gatewayv1.GatewayReasonProgrammed)
	} else {
		programmed.Status = metav1.ConditionFalse
		programmed.Reason = string(DependenciesNotReadyReason)
		programmed.Message = DependenciesNotReadyMessage
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

func areAllConditionsHaveTrueStatus(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(gatewayv1.GatewayConditionProgrammed) {
			continue
		}
		if condition.Type != string(ReadyType) && condition.Status != metav1.ConditionTrue {
			return false
		}
	}
	return true
}

// IsAccepted evaluates whether a resource is in Accepted state, meaning
// that all its listeners are accepted.
func IsAccepted(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(gatewayv1.GatewayConditionAccepted) {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

// IsReady evaluates whether a resource is in Ready state, meaning
// that all its conditions are in the True state.
func IsReady(resource ConditionsAware) bool {
	for _, condition := range resource.GetConditions() {
		if condition.Type == string(ReadyType) {
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
func NewCondition(cType ConditionType, status metav1.ConditionStatus, reason ConditionReason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               string(cType),
		Reason:             string(reason),
		Message:            message,
		LastTransitionTime: metav1.Now(),
		Status:             status,
	}
}

// NewConditionWithGeneration convenience method for creating conditions with ObservedGeneration set.
func NewConditionWithGeneration(cType ConditionType, status metav1.ConditionStatus, reason ConditionReason, message string, observedGeneration int64) metav1.Condition {
	c := NewCondition(cType, status, reason, message)
	c.ObservedGeneration = observedGeneration
	return c
}

// NeedsUpdate retrieves the persisted state and compares all the conditions
// to decide whether the status must be updated or not
func NeedsUpdate(current, updated ConditionsAware) bool {
	if len(current.GetConditions()) != len(updated.GetConditions()) {
		return true
	}

	for _, c := range current.GetConditions() {
		u, exists := GetCondition(ConditionType(c.Type), updated)
		if !exists {
			return true
		}
		if u.Reason != c.Reason || u.Message != c.Message || u.Status != c.Status || u.ObservedGeneration != c.ObservedGeneration {
			return true
		}
	}
	return false
}
