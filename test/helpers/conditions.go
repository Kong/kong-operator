package helpers

import (
	"fmt"
	"strings"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
)

// CheckAllConditionsTrue returns true if all the conditions with given type in `conditionTypes` are set to `True` in the given resource.
// If it returns `false`, the second return value contains a message to tell what conditions are not `True`.
func CheckAllConditionsTrue(resource k8sutils.ConditionsAware, conditionTypes []kcfgconsts.ConditionType) (bool, string) {
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
