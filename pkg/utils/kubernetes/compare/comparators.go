package compare

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// ControlPlaneDeploymentOptionsDeepEqual checks if DeploymentOptions are equal, ignoring some envvars.
func ControlPlaneDeploymentOptionsDeepEqual(o1, o2 *operatorv1beta1.ControlPlaneDeploymentOptions, envVarsToIgnore ...string) bool {
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
		cmp.Comparer(k8sresources.ResourceRequirementsEqual),
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

// NetworkOptionsDeepEqual checks if NetworkOptions are equal.
func NetworkOptionsDeepEqual(opts1, opts2 *operatorv1beta1.DataPlaneNetworkOptions) bool {
	return reflect.DeepEqual(opts1.Services, opts2.Services)
}

// DataPlaneResourceOptionsDeepEqual checks if DataPlane resource options are equal.
func DataPlaneResourceOptionsDeepEqual(opts1, opts2 *operatorv1beta1.DataPlaneResources) bool {
	return reflect.DeepEqual(opts1, opts2)
}
