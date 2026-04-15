package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
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

// LabelObjectAsMCPServerManaged ensures that labels are set on the
// provided object to signal that it's owned by an MCPServer resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsMCPServerManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.MCPServerManagedByLabelValue)
}

// LabelObjectAsKEGDataPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a KEG DataPlane resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsKEGDataPlaneManaged(obj metav1.Object) {
	SetLabel(obj, consts.GatewayOperatorManagedByLabel, consts.KEGDataPlaneManagedByLabelValue)
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
	case *konnectv1alpha1.MCPServer:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.MCPServerManagedByLabelValue,
		}
	case *eventgatewayv1alpha1.KegDataPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.KEGDataPlaneManagedByLabelValue,
		}
	}
	return client.MatchingLabels{}
}
