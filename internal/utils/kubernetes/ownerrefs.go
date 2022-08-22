package kubernetes

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		Controller: pointer.BoolPtr(true),
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

// IsOwnedBy is a helper function to check if the provided object is owned by
// the provided ref UID.
func IsOwnedByRefUID(obj metav1.Object, uid types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
