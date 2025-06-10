package controlplane

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/api/gateway-operator/controlplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
)

// -----------------------------------------------------------------------------
// Reconciler - Status Management
// -----------------------------------------------------------------------------

func (r *Reconciler) ensureIsMarkedScheduled(
	cp *ControlPlane,
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
	cp *ControlPlane,
	dataplane *operatorv1beta1.DataPlane,
) (dataplaneIsSet bool, err error) {
	switch cp.Spec.DataPlane.Type {
	case operatorv2alpha1.ControlPlaneDataPlaneTargetRefType:
		dataplaneIsSet = cp.Spec.DataPlane.Ref != nil && cp.Spec.DataPlane.Ref.Name == dataplane.Name

		var newCondition metav1.Condition
		if dataplaneIsSet {
			newCondition = k8sutils.NewCondition(
				kcfgcontrolplane.ConditionTypeProvisioned,
				metav1.ConditionFalse,
				kcfgcontrolplane.ConditionReasonPodsNotReady,
				"DataPlane was set, ControlPlane resource is scheduled for provisioning",
			)
		} else {
			newCondition = k8sutils.NewCondition(
				kcfgcontrolplane.ConditionTypeProvisioned,
				metav1.ConditionFalse,
				kcfgcontrolplane.ConditionReasonNoDataPlane,
				"DataPlane is not set",
			)
		}

		condition, present := k8sutils.GetCondition(kcfgcontrolplane.ConditionTypeProvisioned, cp)
		if !present || condition.Status != newCondition.Status || condition.Reason != newCondition.Reason {
			k8sutils.SetCondition(newCondition, cp)
		}
		return dataplaneIsSet, nil

	// TODO(pmalek): implement DataPlane URL type

	default:
		return false, fmt.Errorf("unsupported ControlPlane's DataPlane type: %s", cp.Spec.DataPlane.Type)
	}
}
