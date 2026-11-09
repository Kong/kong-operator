package patch

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
)

// ApplyPatchIfNotEmpty patches the provided resource if the resulting patch
// between the provided existingResource and the provided oldExistingResource
// is non empty.
func ApplyPatchIfNotEmpty[
	T client.Object,
](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	existingResource T,
	oldExistingResource T,
	updated bool,
) (res op.Result, deploy T, err error) {
	kind := existingResource.GetObjectKind().GroupVersionKind().Kind

	if !updated {
		log.Trace(logger, "No need for update", "kind", kind, "name", existingResource.GetName())
		return op.Noop, existingResource, nil
	}

	// Check if the patch to be applied is empty.
	patch := client.MergeFrom(oldExistingResource)
	b, err := patch.Data(existingResource)
	if err != nil {
		var t T
		return op.Noop, t, fmt.Errorf("failed to generate patch for %s %s: %w", kind, existingResource.GetName(), err)
	}
	// Only perform the patch operation if the resulting patch is non empty.
	if len(b) == 0 || bytes.Equal(b, []byte("{}")) {
		log.Trace(logger, "No need for update", "kind", kind, "name", existingResource.GetName())
		return op.Noop, existingResource, nil
	}

	if err := cl.Patch(ctx, existingResource, patch); err != nil {
		var t T
		return op.Noop, t, fmt.Errorf("failed patching %s %s: %w", kind, existingResource.GetName(), err)
	}
	log.Debug(logger, "Resource modified", "kind", kind, "name", existingResource.GetName())
	return op.Updated, existingResource, nil
}

// ApplyStatusPatchIfNotEmpty patches the provided object if the resulting patch
// between the provided existing and oldExisting is non empty.
// Extra merge options, such as client.MergeFromWithOptimisticLock, are passed to
// the merge patch.
func ApplyStatusPatchIfNotEmpty[
	T client.Object,
](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	existing T,
	oldExisting T,
	opts ...client.MergeFromOption,
) (res op.Result, err error) {
	// Check if the patch to be applied is empty.
	//
	// The check ignores the resourceVersion: an optimistic lock adds it to the
	// patch even when nothing else changed, and a status write earlier in the
	// same reconcile leaves the object with a newer version than the base.
	// Writing the version alone would bump the resourceVersion on every
	// reconcile, and that write and its watch event would keep re-enqueueing
	// the object.
	probeBase := oldExisting.DeepCopyObject().(T)
	probeBase.SetResourceVersion(existing.GetResourceVersion())

	// NOTE: Code below is not 100% correct as it generates a diff for whole object,
	// not only for status subresource. However, controller-runtime does not provide
	// an easy way to do that, so for now we rely on the fact that only status is
	// being modified before calling this function.
	// In future we might want to implement a custom patcher that would only
	// consider status field.
	b, err := client.MergeFrom(probeBase).Data(existing)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to generate patch for %T %s: %w",
			existing, client.ObjectKeyFromObject(existing), err,
		)
	}
	// Only perform the patch operation if the resulting patch is non empty.
	if len(b) == 0 || bytes.Equal(b, []byte("{}")) {
		log.Trace(logger, "No need for status patch")
		return op.Noop, nil
	}

	if err := cl.Status().Patch(ctx, existing, client.MergeFromWithOptions(oldExisting, opts...)); err != nil {
		return op.Noop, fmt.Errorf("failed patching %T, %s: %w",
			existing, client.ObjectKeyFromObject(existing), err,
		)
	}
	log.Debug(logger, "Resource modified")
	return op.Updated, nil
}
