package reduce

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// OwnedObjectPreDeleteHook is a pre-delete hook for DataPlane-owned objects that ensures that before the
// operator attempts to delete the object, it removes the finalizer that prevents the object from being deleted
// accidentally by users.
func OwnedObjectPreDeleteHook(ctx context.Context, cl client.Client, obj client.Object) error {
	// If there's no finalizer, we don't need to do anything.
	if !slices.Contains(obj.GetFinalizers(), consts.DataPlaneOwnedWaitForOwnerFinalizer) {
		return nil
	}

	// Objects passed to this hook usually come from the informer cache and can be stale by the
	// time the hook runs. Re-read the object and retry on conflict so that a concurrent update
	// (for example, a status or label update by another controller) doesn't make removing the
	// finalizer fail the whole reconcile.
	key := client.ObjectKeyFromObject(obj)
	var replaced bool
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := newEmptyObject(obj)
		if err := cl.Get(ctx, key, current); err != nil {
			return err
		}

		// The original object is gone and another one with the same name has been created
		// in the meantime. Do not remove the finalizer from the replacement object, and
		// return a conflict so the caller does not proceed to delete it.
		if obj.GetUID() != "" && current.GetUID() != obj.GetUID() {
			replaced = true
			return nil
		}

		// Another actor may have removed the finalizer while we were retrying.
		if !slices.Contains(current.GetFinalizers(), consts.DataPlaneOwnedWaitForOwnerFinalizer) {
			return nil
		}

		base := current.DeepCopyObject().(client.Object)
		current.SetFinalizers(slices.DeleteFunc(current.GetFinalizers(), func(finalizer string) bool {
			return finalizer == consts.DataPlaneOwnedWaitForOwnerFinalizer
		}))
		return cl.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	})
	if replaced {
		return fmt.Errorf("failed to remove %q finalizer before deletion: %w",
			consts.DataPlaneOwnedWaitForOwnerFinalizer,
			apierrors.NewConflict(
				groupResourceForObject(obj),
				obj.GetName(),
				errors.New("object UID changed before removing finalizer"),
			),
		)
	}
	// The object is already gone, which is exactly what the caller wants.
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to remove %q finalizer before deletion: %w", consts.DataPlaneOwnedWaitForOwnerFinalizer, err)
	}
	return nil
}

// newEmptyObject allocates a zero-valued client.Object of the same concrete type as obj
// so that uncached client.Get JSON unmarshaling cannot retain stale fields or map entries from obj.
func newEmptyObject(obj client.Object) client.Object {
	empty := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
	if gvk := obj.GetObjectKind().GroupVersionKind(); !gvk.Empty() {
		empty.GetObjectKind().SetGroupVersionKind(gvk)
	}
	return empty
}

func groupResourceForObject(obj client.Object) schema.GroupResource {
	gvk := obj.GetObjectKind().GroupVersionKind()
	resource := gvk.Kind
	if resource == "" {
		resource = reflect.TypeOf(obj).Elem().Name()
	}
	return schema.GroupResource{
		Group:    gvk.Group,
		Resource: resource,
	}
}
