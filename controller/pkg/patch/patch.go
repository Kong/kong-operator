package patch

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/op"
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
func ApplyStatusPatchIfNotEmpty[
	T client.Object,
](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	existing T,
	oldExisting T,
) (res op.Result, err error) {
	// Check if the patch to be applied is empty.
	patch := client.MergeFrom(oldExisting)
	b, err := patch.Data(existing)
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

	if err := cl.Status().Patch(ctx, existing, patch); err != nil {
		return op.Noop, fmt.Errorf("failed patching %T, %s: %w",
			existing, client.ObjectKeyFromObject(existing), err,
		)
	}
	log.Debug(logger, "Resource modified")
	return op.Updated, nil
}
