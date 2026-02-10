package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectWithControlPlaneRef is an interface for objects that have a ControlPlaneRef
// and support deepcopy and condition setting.
type ObjectWithControlPlaneRef[T any] interface {
	client.Object
	DeepCopy() T
	SetConditions([]metav1.Condition)
}
