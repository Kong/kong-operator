package v1alpha1

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// consumerGroupKind is the kind of the sibling reference that supports cross-namespace
// references on the on-prem path: AIGatewayACLRef targets an AIGatewayConsumerGroup.
const consumerGroupKind = "AIGatewayConsumerGroup"

// crossNamespaceSiblingRefsOwner is the generated accessor set of the entities that
// declare sibling references supporting cross-namespace references.
type crossNamespaceSiblingRefsOwner interface {
	CrossNamespaceSiblingReferences() []CrossNamespaceReferenceCheck
}

// CheckCrossNamespaceConsumerGroupRefsGranted returns an error when a KongReferenceGrant
// in a consumer group's namespace does not allow one of the entity's cross-namespace
// consumer group ACL references. It is the same rule as the Konnect path
// (crossnamespace.CheckSiblingReferences), narrowed to the consumer group kind: the
// other sibling references the generator opted in (the certificates) are not supported
// across namespaces on the on-prem path.
func CheckCrossNamespaceConsumerGroupRefsGranted(ctx context.Context, cl client.Client, obj client.Object) error {
	withRefs, ok := obj.(crossNamespaceSiblingRefsOwner)
	if !ok {
		return nil
	}
	var errs []error
	for _, c := range withRefs.CrossNamespaceSiblingReferences() {
		if c.ToGVK.Kind != consumerGroupKind {
			continue
		}
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx, cl,
			c.FromNamespace, c.ToNamespace, c.ToName,
			c.FromGVK, c.ToGVK,
		); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// HasCrossNamespaceConsumerGroupRefs reports whether the entity declares a consumer
// group ACL reference into another namespace.
func HasCrossNamespaceConsumerGroupRefs(obj client.Object) bool {
	return hasConsumerGroupRefInto(obj, "")
}

// HasCrossNamespaceConsumerGroupRefsInto reports whether the entity declares a consumer
// group ACL reference into namespace.
func HasCrossNamespaceConsumerGroupRefsInto(obj client.Object, namespace string) bool {
	return hasConsumerGroupRefInto(obj, namespace)
}

// hasConsumerGroupRefInto reports whether the entity declares a consumer group ACL
// reference, into namespace when namespace is not empty.
func hasConsumerGroupRefInto(obj client.Object, namespace string) bool {
	withRefs, ok := obj.(crossNamespaceSiblingRefsOwner)
	if !ok {
		return false
	}
	for _, c := range withRefs.CrossNamespaceSiblingReferences() {
		if c.ToGVK.Kind == consumerGroupKind && (namespace == "" || c.ToNamespace == namespace) {
			return true
		}
	}
	return false
}
