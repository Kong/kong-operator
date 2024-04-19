package controlplane

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// markAsProvisioned marks the provided resource as ready by the means of Provisioned
// Status Condition.
func markAsProvisioned[T *operatorv1beta1.ControlPlane](resource T) {
	cp, ok := any(resource).(*operatorv1beta1.ControlPlane)
	if ok {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				ConditionTypeProvisioned,
				metav1.ConditionTrue,
				ConditionReasonPodsReady,
				"pods for all Deployments are ready",
				cp.Generation,
			),
			cp,
		)
	}
}
