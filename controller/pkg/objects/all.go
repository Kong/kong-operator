package objects

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteAll deletes all the given objects and returns whether at least one object
// was deleted and an error if any of the deletions failed.
func DeleteAll[
	Object any,
	ObjectPtr interface {
		*Object
		client.Object
	},
](ctx context.Context, c client.Client, objs []Object) (bool, error) {
	var (
		errs    []error
		deleted bool
	)
	for i := range objs {
		var objPtr ObjectPtr = &objs[i]

		// skip already deleted objects, because objects may have finalizers
		// to wait for owned cluster wide resources deleted.
		if !objPtr.GetDeletionTimestamp().IsZero() {
			continue
		}

		err := c.Delete(ctx, objPtr)
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	if len(errs) > 0 {
		return deleted,
			fmt.Errorf("failed to delete some objects: %w", errors.Join(errs...))
	}

	return deleted, nil
}
