package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	internalgatewayapi "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	internal "github.com/kong/kong-operator/ingress-controller/internal/util"
)

type ConditionType = internal.ConditionType
type ConditionReason = internal.ConditionReason

func StringToGatewayAPIKindPtr(kind string) *internalgatewayapi.Kind {
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
