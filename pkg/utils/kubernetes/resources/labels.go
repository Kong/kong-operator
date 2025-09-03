package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/apis/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// LabelObjectAsDataPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a DataPlane resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsDataPlaneManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.DataPlaneManagedLabelValue)
}

// LabelObjectAsKongPluginInstallationManaged ensures that labels are set on the
// provided object to signal that it's owned by a KongPluginInstallation
// resource and that its lifecycle is managed by this operator.
func LabelObjectAsKongPluginInstallationManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.KongPluginInstallationManagedLabelValue)
}

// LabelObjectAsKonnectExtensionManaged ensures that labels are set on the
// provided object to signal that it's owned by a KonnectExtension resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsKonnectExtensionManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.KonnectExtensionManagedByLabelValue)
}

// LabelObjectAsControlPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a ControlPlane resource and that its
// lifecycle is managed by this operator.
func LabelObjectAsControlPlaneManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.ControlPlaneManagedLabelValue)
}

// SetLabel sets a label on the provided object.
func SetLabel(obj metav1.Object, key string, value string) {
	if key == "" || value == "" {
		return
	}
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[key] = value
	obj.SetLabels(labels)
}

// GetManagedLabelForOwner returns the managed-by labels for the provided owner.
func GetManagedLabelForOwner(owner metav1.Object) client.MatchingLabels {
	switch owner.(type) {
	case *gwtypes.ControlPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		}
	case *operatorv1beta1.DataPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}
	case *konnectv1alpha2.KonnectExtension:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
		}
	}
	return client.MatchingLabels{}
}
