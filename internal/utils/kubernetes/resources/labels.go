package resources

import (
	"fmt"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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
	labels[consts.GatewayOperatorManagedByLabelLegacy] = consts.DataPlaneManagedLabelValue
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
	labels[consts.GatewayOperatorManagedByLabelLegacy] = consts.ControlPlaneManagedLabelValue
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

func GetManagedLabelRequirementsForOwnerLegacy(owner metav1.Object) (labels.Requirements, error) {
	managedByLabelsLegacy := GetManagedLabelForOwnerLegacy(owner)
	if len(managedByLabelsLegacy) == 0 {
		return nil, fmt.Errorf("no legacy managed-by labels for owner %s", owner.GetName())
	}
	reqLegacy, err := labels.NewRequirement(
		lo.Keys(managedByLabelsLegacy)[0], selection.Equals, lo.Values(managedByLabelsLegacy),
	)
	if err != nil {
		return nil, err
	}

	managedByLabels := GetManagedLabelForOwner(owner)
	if len(managedByLabels) == 0 {
		return nil, fmt.Errorf("no managed-by labels for owner %s", owner.GetName())
	}
	req, err := labels.NewRequirement(lo.Keys(managedByLabels)[0], selection.DoesNotExist, []string{})
	if err != nil {
		return nil, err
	}

	return labels.Requirements{*req, *reqLegacy}, nil
}
