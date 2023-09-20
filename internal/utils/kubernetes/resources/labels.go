package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
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
	labels[consts.GatewayOperatorManagedByLabel] = consts.DataPlaneManagedLabelValue
	// TODO: Remove adding this to managed resources after several versions with
	// the new managed-by label were released: https://github.com/Kong/gateway-operator/issues/1101
	labels[consts.GatewayOperatorManagedByLabelLegacy] = consts.DataPlaneManagedLabelValue //nolint:staticcheck
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
	labels[consts.GatewayOperatorManagedByLabel] = consts.ControlPlaneManagedLabelValue
	// TODO: Remove adding this to managed resources after several versions with
	// the new managed-by label were released: https://github.com/Kong/gateway-operator/issues/1101
	labels[consts.GatewayOperatorManagedByLabelLegacy] = consts.ControlPlaneManagedLabelValue //nolint:staticcheck
	obj.SetLabels(labels)
}

// GetManagedLabelForOwner returns the managed-by labels for the provided owner.
func GetManagedLabelForOwner(owner metav1.Object) client.MatchingLabels {
	switch owner.(type) {
	case *operatorv1alpha1.ControlPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		}
	case *operatorv1beta1.DataPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}
	}
	return client.MatchingLabels{}
}

// GetManagedLabelForOwnerLegacy returns the legacy managed-by labels for the
// provided owner.
//
// Deprecated: use getManagedLabelForOwner instead.
// Removed when https://github.com/Kong/gateway-operator/issues/1101 is closed.
func GetManagedLabelForOwnerLegacy(owner metav1.Object) client.MatchingLabels {
	switch owner.(type) {
	case *operatorv1alpha1.ControlPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabelLegacy: consts.ControlPlaneManagedLabelValue,
		}
	case *operatorv1beta1.DataPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabelLegacy: consts.DataPlaneManagedLabelValue,
		}
	}
	return client.MatchingLabels{}
}
