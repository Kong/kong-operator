package metadata

import (
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// NamespaceFromParentRef returns the namespace for a given Route ParentReference.
// If the ParentReference specifies a namespace, that namespace is returned.
// Otherwise, the namespace of the provided resource is returned.
func NamespaceFromParentRef(obj metav1.Object, pRef *gwtypes.ParentReference) string {
	var ns gatewayv1.Namespace

	if pRef == nil {
		return obj.GetNamespace()
	}
	if ns = lo.FromPtr(pRef.Namespace); ns == "" {
		return obj.GetNamespace()
	}
	return string(ns)
}
