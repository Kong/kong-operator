package controlplane

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// markAsProvisioned marks the provided resource as ready by the means of Provisioned
// Status Condition.
func markAsProvisioned[T *operatorv1beta1.ControlPlane](resource T) {
	switch resource := any(resource).(type) {
	case *operatorv1beta1.ControlPlane:
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				ConditionTypeProvisioned,
				metav1.ConditionTrue,
				ConditionReasonPodsReady,
				"pods for all Deployments are ready",
				resource.Generation,
			),
			resource,
		)
	}
}
