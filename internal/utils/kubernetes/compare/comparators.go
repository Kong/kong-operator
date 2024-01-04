package compare

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

func DeploymentOptionsV1AlphaDeepEqual(o1, o2 *operatorv1alpha1.DeploymentOptions, envVarsToIgnore ...string) bool {
	if o1 == nil && o2 == nil {
		return true
	}

	if (o1 == nil && o2 != nil) || (o1 != nil && o2 == nil) {
		return false
	}

	if !reflect.DeepEqual(o1.Replicas, o2.Replicas) {
		return false
	}

	opts := []cmp.Option{
		cmp.Comparer(func(a, b corev1.ResourceRequirements) bool {
			return resources.ResourceRequirementsEqual(a, b)
		}),
		cmp.Comparer(func(a, b []corev1.EnvVar) bool {
			// Throw out env vars that we ignore.
			a = lo.Filter(a, func(e corev1.EnvVar, _ int) bool {
				return !lo.Contains(envVarsToIgnore, e.Name)
			})
			b = lo.Filter(b, func(e corev1.EnvVar, _ int) bool {
				return !lo.Contains(envVarsToIgnore, e.Name)
			})

			// And compare.
			return reflect.DeepEqual(a, b)
		}),
	}
	return cmp.Equal(&o1.PodTemplateSpec, &o2.PodTemplateSpec, opts...)
}
