package metadata

import (
	gwtypes "github.com/kong/kong-operator/internal/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamespaceFromParentRef returns the namespace for a given Route ParentReference.
// If the ParentReference specifies a namespace, that namespace is returned.
// Otherwise, the namespace of the provided resource is returned.
func NamespaceFromParentRef(obj metav1.Object, pRef *gwtypes.ParentReference) string {
	if pRef.Namespace != nil && *pRef.Namespace != "" {
		return string(*pRef.Namespace)
	}
	return obj.GetNamespace()
}
