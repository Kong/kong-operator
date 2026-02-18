package dataplane

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// OwnedObjectPreDeleteHook is a pre-delete hook for DataPlane-owned objects that ensures that before the
// operator attempts to delete the object, it removes the finalizer that prevents the object from being deleted
// accidentally by users.
func OwnedObjectPreDeleteHook(ctx context.Context, cl client.Client, obj client.Object) error {
	finalizers := obj.GetFinalizers()

	// If there's no finalizer, we don't need to do anything.
	if !lo.Contains(finalizers, consts.DataPlaneOwnedWaitForOwnerFinalizer) {
		return nil
	}

	// Otherwise, we delete the finalizer and update the object.
	obj.SetFinalizers(lo.Reject(finalizers, func(s string, _ int) bool {
		return s == consts.DataPlaneOwnedWaitForOwnerFinalizer
	}))
	if err := cl.Update(ctx, obj); err != nil {
		return fmt.Errorf("failed to remove %q finalizer before deletion: %w", consts.DataPlaneOwnedWaitForOwnerFinalizer, err)
	}
	return nil
}
