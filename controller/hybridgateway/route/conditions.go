package route

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	routeconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/route"
)

// GetProgrammedConditionForGVK returns a programmed condition for the given GVK, set to True or False
// based on the programmed argument. It matches the GVK to the appropriate condition type and reason.
func GetProgrammedConditionForGVK(gvk schema.GroupVersionKind, programmed bool) metav1.Condition {
	var condType, reasonProgrammed, reasonNotProgrammed string
	switch gvk.Kind {
	case "KongRoute":
		condType = routeconst.ConditionTypeKongRouteProgrammed
		reasonProgrammed = routeconst.ConditionReasonKongRouteProgrammed
		reasonNotProgrammed = routeconst.ConditionReasonKongRouteNotProgrammed
	case "KongService":
		condType = routeconst.ConditionTypeKongServiceProgrammed
		reasonProgrammed = routeconst.ConditionReasonKongServiceProgrammed
		reasonNotProgrammed = routeconst.ConditionReasonKongServiceNotProgrammed
	case "KongTarget":
		condType = routeconst.ConditionTypeKongTargetProgrammed
		reasonProgrammed = routeconst.ConditionReasonKongTargetProgrammed
		reasonNotProgrammed = routeconst.ConditionReasonKongTargetNotProgrammed
	case "KongUpstream":
		condType = routeconst.ConditionTypeKongUpstreamProgrammed
		reasonProgrammed = routeconst.ConditionReasonKongUpstreamProgrammed
		reasonNotProgrammed = routeconst.ConditionReasonKongUpstreamNotProgrammed
	case "KongPluginBinding":
		condType = routeconst.ConditionTypeKongPluginBindingProgrammed
		reasonProgrammed = routeconst.ConditionReasonKongPluginBindingProgrammed
		reasonNotProgrammed = routeconst.ConditionReasonKongPluginBindingNotProgrammed
	default:
		// Unknown kind, return empty condition
		return metav1.Condition{}
	}

	var status metav1.ConditionStatus
	var reason, message string
	if programmed {
		status = metav1.ConditionTrue
		reason = reasonProgrammed
		message = "Resource is programmed"
	} else {
		status = metav1.ConditionFalse
		reason = reasonNotProgrammed
		message = "Resource is not programmed"
	}

	return metav1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

// DeduplicateConditionsByType returns a slice of conditions with unique types, keeping the most severe status for each type.
// Severity order: False (most severe) > Unknown > True (least severe). If multiple conditions of the same type exist, the most severe is kept.
func DeduplicateConditionsByType(conditions []metav1.Condition) []metav1.Condition {
	condMap := make(map[string]metav1.Condition)
	for _, cond := range conditions {
		existing, exists := condMap[cond.Type]
		if !exists || conditionSeverity(cond.Status) < conditionSeverity(existing.Status) {
			condMap[cond.Type] = cond
		}
	}
	uniqueConditions := make([]metav1.Condition, 0, len(condMap))
	for _, cond := range condMap {
		uniqueConditions = append(uniqueConditions, cond)
	}
	return uniqueConditions
}

// conditionSeverity returns an integer representing the severity of a condition status.
// False = 0 (most severe), Unknown = 1, True = 2 (least severe).
func conditionSeverity(status metav1.ConditionStatus) int {
	switch status {
	case metav1.ConditionFalse:
		return 0
	case metav1.ConditionUnknown:
		return 1
	case metav1.ConditionTrue:
		return 2
	default:
		return 0
	}
}
