package envtest

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func ConditionsAreSetWhenReferencedControlPlaneIsMissing[
	T interface {
		client.Object
		k8sutils.ConditionsAware
		GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
	},
](objToMatch T) func(obj T) bool {
	return func(obj T) bool {
		if obj.GetName() != objToMatch.GetName() {
			return false
		}
		if obj.GetControlPlaneRef().Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef {
			return false
		}
		condCpRef, okCPRef := k8sutils.GetCondition(konnectv1alpha1.ControlPlaneRefValidConditionType, obj)
		condProgrammed, okProgrammed := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, obj)
		return okCPRef && okProgrammed &&
			condCpRef.Status == "False" &&
			condProgrammed.Status == "False" &&
			condCpRef.Reason == konnectv1alpha1.ControlPlaneRefReasonInvalid &&
			condProgrammed.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists
	}
}

func ConditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef[
	T interface {
		client.Object
		k8sutils.ConditionsAware
		GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
		GetKonnectID() string
	},
](objToMatch T, id string) func(T) bool {
	return func(obj T) bool {
		if obj.GetName() != objToMatch.GetName() {
			return false
		}
		if obj.GetControlPlaneRef().Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef {
			return false
		}
		if obj.GetKonnectID() != id {
			return false
		}

		return k8sutils.IsProgrammed(obj)
	}
}

// ObjectHasConditionProgrammedSetToTrue returns a function that checks if the given
// object has the "Programmed" condition set to true.
func ObjectHasConditionProgrammedSetToTrue[
	T k8sutils.ConditionsAware,
]() func(T) bool {
	return func(obj T) bool {
		return k8sutils.IsProgrammed(obj)
	}
}

func ConditionsContainProgrammedFalse(conds []metav1.Condition) bool {
	return ConditionsContainProgrammed(conds, metav1.ConditionFalse)
}

func ConditionsContainProgrammedTrue(conds []metav1.Condition) bool {
	return ConditionsContainProgrammed(conds, metav1.ConditionTrue)
}

func ConditionsContainProgrammed(conds []metav1.Condition, status metav1.ConditionStatus) bool {
	return lo.ContainsBy(conds,
		func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == status
		},
	)
}
