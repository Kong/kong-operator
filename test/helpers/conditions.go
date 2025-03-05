package helpers

import (
	"fmt"
	"strings"

	"github.com/samber/lo"

	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// CheckAllConditionsTrue returns true if all the conditions with given type in `conditionTypes` are set to `True` in the given resource.
// If it returns `false`, the second return value contains a message to tell what conditions are not `True`.
func CheckAllConditionsTrue(resource k8sutils.ConditionsAware, conditionTypes []consts.ConditionType) (bool, string) {
	var failedConditions []string
	lo.ForEach(conditionTypes, func(conditionType consts.ConditionType, _ int) {
		if !k8sutils.HasConditionTrue(conditionType, resource) {
			failedConditions = append(failedConditions, string(conditionType))
		}
	})
	if len(failedConditions) > 0 {
		return false, fmt.Sprintf("condition(s) %s not set to True", strings.Join(failedConditions, ", "))
	}
	return true, ""
}
