package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// ObjectWithControlPlaneRef is an interface for objects that have a ControlPlaneRef
// and support deepcopy and condition setting.
type ObjectWithControlPlaneRef[T any] interface {
	client.Object
	DeepCopy() T
	SetConditions([]metav1.Condition)
	SetControlPlaneRef(*commonv1alpha1.ControlPlaneRef)
	GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
}
