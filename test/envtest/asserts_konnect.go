package envtest

import (
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

func objectHasCPRefKonnectID[
	T interface {
		GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
	},
]() func(T) bool {
	return func(obj T) bool {
		return obj.GetControlPlaneRef().Type == commonv1alpha1.ControlPlaneRefKonnectID
	}
}

func objectMatchesKonnectID[
	T interface {
		GetKonnectID() string
	},
](id string) func(T) bool {
	return func(obj T) bool {
		return obj.GetKonnectID() == id
	}
}
