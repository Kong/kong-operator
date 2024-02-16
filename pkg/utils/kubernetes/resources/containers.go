package resources

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

// IsContainerResourceEmpty determines if the provided resource requirements is effectively
// "empty" in that all fields are unset.
func IsContainerResourceEmpty(resources corev1.ResourceRequirements) bool {
	return reflect.DeepEqual(resources, corev1.ResourceRequirements{})
}
