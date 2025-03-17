package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// LabelObjectAsDataPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a DataPlane resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsDataPlaneManaged(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorManagedByLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}

// LabelObjectAsDataPlaneManaged ensures that labels are set on the
// provided object to signal that it's owned by a KongPluginInstallation
// resource and that its lifecycle is managed by this operator.
func LabelObjectAsKongPluginInstallationManaged(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorManagedByLabel] = consts.KongPluginInstallationManagedLabelValue
	obj.SetLabels(labels)
}

// LabelObjectAsKonnectExtensionManaged ensures that labels are set on the
// provided object to signal that it's owned by a KonnectExtension resource
// and that its lifecycle is managed by this operator.
func LabelObjectAsKonnectExtensionManaged(obj metav1.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorManagedByLabel] = consts.KonnectExtensionManagedByLabelValue
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
	obj.SetLabels(labels)
}

// GetManagedLabelForOwner returns the managed-by labels for the provided owner.
func GetManagedLabelForOwner(owner metav1.Object) client.MatchingLabels {
	switch owner.(type) {
	case *operatorv1beta1.ControlPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		}
	case *operatorv1beta1.DataPlane:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}
	case *konnectv1alpha1.KonnectExtension:
		return client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.KonnectExtensionManagedByLabelValue,
		}
	}
	return client.MatchingLabels{}
}
