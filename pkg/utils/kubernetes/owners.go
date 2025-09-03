package kubernetes

import (
	"fmt"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// -----------------------------------------------------------------------------
// Kubernetes Utils - Owner References
// -----------------------------------------------------------------------------

// GenerateOwnerReferenceForObject provides a metav1.OwnerReference for the
// provided object so that it can be applied to other objects to indicate
// ownership by the given object.
func GenerateOwnerReferenceForObject(obj client.Object) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: GetAPIVersionForObject(obj),
		Kind:       obj.GetObjectKind().GroupVersionKind().Kind,
		Name:       obj.GetName(),
		UID:        obj.GetUID(),
		Controller: lo.ToPtr(true),
	}
}

// SetOwnerForObject ensures that the provided first object is marked as
// owned by the provided second object in the object metadata.
func SetOwnerForObject(obj, owner client.Object) {
	foundOwnerRef := false
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.UID == owner.GetUID() {
			foundOwnerRef = true
		}
	}
	if !foundOwnerRef {
		obj.SetOwnerReferences(append(obj.GetOwnerReferences(), GenerateOwnerReferenceForObject(owner)))
	}
}

// managingObjectT is type constraint that is used to represent a managing object.
// Currently it can be one of: Gateway, ControlPlane, or DataPlane.
type managingObjectT interface {
	client.Object

	*gatewayv1.Gateway |
		*gwtypes.ControlPlane |
		*operatorv1beta1.DataPlane
}

// GetManagedByLabelSet returns a map of labels with the provided object's metadata.
// These can be applied to other objects that are owned by the object provided as an argument.
func GetManagedByLabelSet[managingObject managingObjectT](object managingObject) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          getKindLabel(object),
		consts.GatewayOperatorManagedByNamespaceLabel: object.GetNamespace(),
		consts.GatewayOperatorManagedByNameLabel:      object.GetName(),
	}
}

func getKindLabel[managingObject managingObjectT](object managingObject) string {
	switch any(object).(type) {
	case *gatewayv1.Gateway:
		return consts.GatewayManagedLabelValue
	case *gwtypes.ControlPlane:
		return consts.ControlPlaneManagedLabelValue
	case *operatorv1beta1.DataPlane:
		return consts.DataPlaneManagedLabelValue
	default:
		return fmt.Sprintf("unknown-kind-%s", object.GetObjectKind().GroupVersionKind().Kind)
	}
}

// GetOwnerReferencer retrieves owner references.
type GetOwnerReferencer interface {
	GetOwnerReferences() []metav1.OwnerReference
}

// IsOwnedByRefUID is a helper function to check if the provided object is owned by
// the provided ref UID.
func IsOwnedByRefUID(obj GetOwnerReferencer, uid types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
