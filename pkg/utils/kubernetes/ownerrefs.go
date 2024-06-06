package kubernetes

import (
	"fmt"

	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"
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

// SetOwnerForObjectThroughLabels sets the owner of the provided object through a label.
func SetOwnerForObjectThroughLabels(obj, owner client.Object) error {
	labels := obj.GetLabels()
	managedByLabelSet, err := GetManagedByLabelSet(owner)
	if err != nil {
		return err
	}
	for k, v := range managedByLabelSet {
		labels[k] = v
	}
	obj.SetLabels(labels)
	return nil
}

// GetManagedByLabelSet returns a map of labels with the managing object's metadata.
func GetManagedByLabelSet(obj client.Object) (map[string]string, error) {
	var kindLabel string
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "Gateway":
		kindLabel = consts.GatewayManagedLabelValue
	case "ControlPlane":
		kindLabel = consts.ControlPlaneManagedLabelValue
	case "DataPlane":
		kindLabel = consts.DataPlaneManagedLabelValue
	default:
		return nil, fmt.Errorf("unsupported owner of kind %q", obj.GetObjectKind().GroupVersionKind().Kind)
	}
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          kindLabel,
		consts.GatewayOperatorManagedByNamespaceLabel: obj.GetNamespace(),
		consts.GatewayOperatorManagedByNameLabel:      obj.GetName(),
	}, nil
}

// GetOwnerReferencer retrieves owner references.
type GetOwnerReferencer interface {
	GetOwnerReferences() []metav1.OwnerReference
}

// IsOwnedBy is a helper function to check if the provided object is owned by
// the provided ref UID.
func IsOwnedByRefUID(obj GetOwnerReferencer, uid types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
