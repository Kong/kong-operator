package controlplane

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/api/gateway-operator/controlplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// -----------------------------------------------------------------------------
// Reconciler - Status Management
// -----------------------------------------------------------------------------

func (r *Reconciler) ensureIsMarkedScheduled(
	cp *operatorv1beta1.ControlPlane,
) bool {
	_, present := k8sutils.GetCondition(kcfgcontrolplane.ConditionTypeProvisioned, cp)
	if !present {
		condition := k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeProvisioned,
			metav1.ConditionFalse,
			kcfgcontrolplane.ConditionReasonPodsNotReady,
			"ControlPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, cp)
		return true
	}

	return false
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane deployments reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *Reconciler) ensureDataPlaneStatus(
	cp *operatorv1beta1.ControlPlane,
	dataplane *operatorv1beta1.DataPlane,
) (dataplaneIsSet bool) {
	dataplaneIsSet = cp.Spec.DataPlane != nil && *cp.Spec.DataPlane == dataplane.Name
	condition, present := k8sutils.GetCondition(kcfgcontrolplane.ConditionTypeProvisioned, cp)

	newCondition := k8sutils.NewCondition(
		kcfgcontrolplane.ConditionTypeProvisioned,
		metav1.ConditionFalse,
		kcfgcontrolplane.ConditionReasonNoDataPlane,
		"DataPlane is not set",
	)
	if dataplaneIsSet {
		newCondition = k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeProvisioned,
			metav1.ConditionFalse,
			kcfgcontrolplane.ConditionReasonPodsNotReady,
			"DataPlane was set, ControlPlane resource is scheduled for provisioning",
		)
	}
	if !present || condition.Status != newCondition.Status || condition.Reason != newCondition.Reason {
		k8sutils.SetCondition(newCondition, cp)
	}
	return dataplaneIsSet
}
