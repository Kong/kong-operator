package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func conditionsAreSetWhenReferencedControlPlaneIsMissing[
	T interface {
		client.Object
		k8sutils.ConditionsAware
		GetControlPlaneRef() *configurationv1alpha1.ControlPlaneRef
	},
](objToMatch T) func(obj T) bool {
	return func(obj T) bool {
		if obj.GetName() != objToMatch.GetName() {
			return false
		}
		if obj.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
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

func conditionProgrammedIsSetToTrue[
	T interface {
		client.Object
		k8sutils.ConditionsAware
		GetControlPlaneRef() *configurationv1alpha1.ControlPlaneRef
		GetKonnectID() string
	},
](objToMatch T, id string) func(T) bool {
	return func(obj T) bool {
		if obj.GetName() != objToMatch.GetName() {
			return false
		}
		if obj.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
			return false
		}
		return obj.GetKonnectID() == id && k8sutils.IsProgrammed(obj)
	}
}
