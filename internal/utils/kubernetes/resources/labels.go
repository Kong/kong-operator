package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/internal/consts"
)

// LabelObjectAsDataPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a DataPlane resource and that its
// lifecycle is managed by this operator.
func LabelObjectAsDataPlaneManaged(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}

// LabelObjectAsControlPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a ControlPlane resource and that its
// lifecycle is managed by this operator.
func LabelObjectAsControlPlaneManaged(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.ControlPlaneManagedLabelValue
	obj.SetLabels(labels)
}
