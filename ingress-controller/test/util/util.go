package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	internal "github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

type ConditionType = internal.ConditionType
type ConditionReason = internal.ConditionReason

func StringToGatewayAPIKindPtr(kind string) *gwtypes.Kind {
	return internal.StringToGatewayAPIKindPtr(kind)
}

func CheckCondition(
	conditions []metav1.Condition,
	typ ConditionType,
	reason ConditionReason,
	status metav1.ConditionStatus,
	resourceGeneration int64,
) bool {
	return internal.CheckCondition(conditions, typ, reason, status, resourceGeneration)
}
