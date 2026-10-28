package v1alpha1

import (
	"k8s.io/apimachinery/pkg/types"
)

// ConsumerNamespacedName returns the namespace/name of the AIGatewayConsumer the credential
// references. The namespace defaults to the credential's own, as the generated index extractor
// and the on-prem translation do. ok is false when the reference carries no namespacedRef:
// the CRD also admits type: konnectID.
//
// The generated index extractor (internal/utils/index) keeps its own copy of the defaulting
// because generated files cannot call it.
func (obj *AIGatewayConsumerCredential) ConsumerNamespacedName() (nn types.NamespacedName, ok bool) {
	ref := obj.Spec.AIGatewayConsumerRef.NamespacedRef
	if ref == nil {
		return types.NamespacedName{}, false
	}
	nn = types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}
	if ref.Namespace != nil && *ref.Namespace != "" {
		nn.Namespace = *ref.Namespace
	}
	return nn, true
}
