package controlplane

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// markAsProvisioned marks the provided resource as ready by the means of Provisioned
// Status Condition.
func markAsProvisioned[T *ControlPlane](resource T) {
	cp, ok := any(resource).(*ControlPlane)
	if ok {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgcontrolplane.ConditionTypeProvisioned,
				metav1.ConditionTrue,
				kcfgcontrolplane.ConditionReasonProvisioned,
				"ControlPlane has been provisioned",
				cp.Generation,
			),
			cp,
		)
	}
}
