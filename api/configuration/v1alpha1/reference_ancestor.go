package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// maxAncestorDepth bounds the walk up the parents of a referenced object.
const maxAncestorDepth = 5

// ancestorSkipsKonnectReconciliation reports whether an ancestor of obj, e.g.
// the backend cluster of a virtual cluster, is one the Konnect reconciler
// doesn't manage (it has a SkipKonnectReconciliation method returning true),
// as one attached to an on-prem gateway. Such an object never gets a Konnect
// ID: a reference to it can't be resolved by waiting for it to be programmed.
// The generated resolvers only call it for objects without a Konnect ID. A
// parent that can't be read, e.g. one that doesn't exist (yet), stops the walk:
// the reference is then reported as not programmed.
func ancestorSkipsKonnectReconciliation(ctx context.Context, cl client.Client, obj client.Object) bool {
	for range maxAncestorDepth {
		child, ok := obj.(interface {
			GetParentRef() commonv1alpha1.ObjectRef
			GetParentGVK() schema.GroupVersionKind
		})
		if !ok {
			return false
		}
		ref := child.GetParentRef()
		if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
			return false
		}
		o, err := cl.Scheme().New(child.GetParentGVK())
		if err != nil {
			return false
		}
		parent, ok := o.(client.Object)
		if !ok {
			return false
		}
		key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: ref.NamespacedRef.Name}
		if ns := ref.NamespacedRef.Namespace; ns != nil && *ns != "" {
			key.Namespace = *ns
		}
		if err := cl.Get(ctx, key, parent); err != nil {
			return false
		}
		if skipper, ok := parent.(interface{ SkipKonnectReconciliation() bool }); ok && skipper.SkipKonnectReconciliation() {
			return true
		}
		obj = parent
	}
	return false
}
