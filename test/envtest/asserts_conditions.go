package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

func conditionsAreSetWhenReferencedControlPlaneIsMissing[
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

func conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef[
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

func objectHasConditionProgrammedSetToTrue[
	T k8sutils.ConditionsAware,
]() func(T) bool {
	return func(obj T) bool {
		return k8sutils.IsProgrammed(obj)
	}
}
