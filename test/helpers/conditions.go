package helpers

import (
	"fmt"
	"strings"

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// ConditionsChecker is a function type that checks the conditions of a resource.
// It takes a resource of type k8sutils.ConditionsAware and a variadic number of condition types.
// It returns a boolean indicating whether all specified conditions are met and a string message.
type ConditionsChecker func(resource k8sutils.ConditionsAware, conditionTypes ...kcfgconsts.ConditionType) (bool, string)

// CheckAllConditionsTrue returns true if all the conditions with given type in `conditionTypes` are set to `True` in the given resource.
// If it returns `false`, the second return value contains a message to tell what conditions are not `True`.
func CheckAllConditionsTrue(resource k8sutils.ConditionsAware, conditionTypes ...kcfgconsts.ConditionType) (bool, string) {
	var failedConditions []string
	for _, conditionType := range conditionTypes {
		if !k8sutils.HasConditionTrue(conditionType, resource) {
			failedConditions = append(failedConditions, string(conditionType))
		}
	}

	if len(failedConditions) > 0 {
		return false, fmt.Sprintf("condition(s) %s not set to True", strings.Join(failedConditions, ", "))
	}
	return true, ""
}

// CheckAllConditionsFalse returns true if all the conditions with given type in `conditionTypes` are set to `False` in the given resource.
// If it returns `false`, the second return value contains a message to tell what conditions are not `False`.
func CheckAllConditionsFalse(resource k8sutils.ConditionsAware, conditionTypes ...kcfgconsts.ConditionType) (bool, string) {
	var failedConditions []string
	for _, conditionType := range conditionTypes {
		if k8sutils.HasConditionTrue(conditionType, resource) {
			failedConditions = append(failedConditions, string(conditionType))
		}
	}

	if len(failedConditions) > 0 {
		return false, fmt.Sprintf("condition(s) %s not set to False", strings.Join(failedConditions, ", "))
	}
	return true, ""
}
