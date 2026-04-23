package annotations

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	serviceUpstreamAnnotation = "ingress.kubernetes.io/service-upstream"
)

// IsServiceUpstream returns true if the annotation
// ingress.kubernetes.io/service-upstream is set to "true" in anns.
func IsServiceUpstream[
	T interface {
		*corev1.Service
		metav1.Object
	},
](obj T) bool {
	if obj == nil {
		return false
	}
	return obj.GetAnnotations()[serviceUpstreamAnnotation] == "true"
}
