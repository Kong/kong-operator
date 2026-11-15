package v1alpha1

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
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

// HasCrossNamespaceConsumerRef reports whether the credential references the consumer from
// another namespace.
func (obj *AIGatewayConsumerCredential) HasCrossNamespaceConsumerRef() bool {
	return obj.HasCrossNamespaceConsumerRefInto("")
}

// HasCrossNamespaceConsumerRefInto reports whether the credential references a consumer in
// namespace from another namespace, for any namespace when namespace is empty.
func (obj *AIGatewayConsumerCredential) HasCrossNamespaceConsumerRefInto(namespace string) bool {
	nn, ok := obj.ConsumerNamespacedName()
	return ok && nn.Namespace != obj.GetNamespace() && (namespace == "" || nn.Namespace == namespace)
}

// CheckCrossNamespaceConsumerRefGranted returns an error when a KongReferenceGrant in the
// consumer's namespace does not allow the credential to reference it. It is the same rule as
// the Konnect path (controller/konnect reconciler_generic_handle_parentref.go
// ensureKongReferenceGrantForParentRef).
func CheckCrossNamespaceConsumerRefGranted(
	ctx context.Context,
	cl client.Client,
	obj *AIGatewayConsumerCredential,
) error {
	nn, ok := obj.ConsumerNamespacedName()
	if !ok || nn.Namespace == obj.GetNamespace() {
		return nil
	}
	return crossnamespace.CheckKongReferenceGrantForResource(
		ctx, cl,
		obj.GetNamespace(),
		nn.Namespace,
		nn.Name,
		metav1.GroupVersionKind(GroupVersion.WithKind(obj.GetTypeName())),
		metav1.GroupVersionKind(GroupVersion.WithKind("AIGatewayConsumer")),
	)
}
