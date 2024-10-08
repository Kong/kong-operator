package ops

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

type entityType interface {
	SetConditions([]metav1.Condition)
	GetConditions() []metav1.Condition
	GetGeneration() int64
}

// SetKonnectEntityProgrammedCondition sets the KonnectEntityProgrammed condition to true
// on the provided object.
func SetKonnectEntityProgrammedCondition(
	obj entityType,
) {
	_setKonnectEntityProgrammedConditon(
		obj,
		metav1.ConditionTrue,
		konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
		"",
	)
}

// SetKonnectEntityProgrammedConditionFalse sets the KonnectEntityProgrammed condition
// to false on the provided object.
func SetKonnectEntityProgrammedConditionFalse(
	obj entityType,
	reason consts.ConditionReason,
	msg string,
) {
	_setKonnectEntityProgrammedConditon(
		obj,
		metav1.ConditionFalse,
		reason,
		msg,
	)
}

func _setKonnectEntityProgrammedConditon(
	obj entityType,
	status metav1.ConditionStatus,
	reason consts.ConditionReason,
	msg string,
) {
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			konnectv1alpha1.KonnectEntityProgrammedConditionType,
			status,
			reason,
			msg,
			obj.GetGeneration(),
		),
		obj,
	)
}
