package controllers

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/controllers/pkg/op"
)

// patchIfPatchIsNonEmpty patches the provided resource if the resulting patch
// between the provided existingResource and the provided oldExistingResource
// is non empty.
func patchIfPatchIsNonEmpty[
	OwnerT *operatorv1beta1.DataPlane | *operatorv1alpha1.ControlPlane,
	ResourceT interface {
		*appsv1.Deployment | *autoscalingv2.HorizontalPodAutoscaler
		client.Object
	},
](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	existingResource ResourceT,
	oldExistingResource ResourceT,
	owner OwnerT,
	updated bool,
) (res op.CreatedUpdatedOrNoop, deploy ResourceT, err error) {
	kind := existingResource.GetObjectKind().GroupVersionKind().Kind

	if !updated {
		log.Trace(logger, "No need for update", owner, kind, existingResource.GetName())
		return op.Noop, existingResource, nil
	}

	// Check if the patch to be applied is empty.
	patch := client.MergeFrom(oldExistingResource)
	b, err := patch.Data(existingResource)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed to generate patch for %s %s: %w", kind, existingResource.GetName(), err)
	}
	// Only perform the patch operation if the resulting patch is non empty.
	if len(b) == 0 || bytes.Equal(b, []byte("{}")) {
		log.Trace(logger, "No need for update", owner, kind, existingResource.GetName())
		return op.Noop, existingResource, nil
	}

	if err := cl.Patch(ctx, existingResource, client.MergeFrom(oldExistingResource)); err != nil {
		return op.Noop, nil, fmt.Errorf("failed patching %s %s: %w", kind, existingResource.GetName(), err)
	}
	log.Debug(logger, "Resource modified", owner, kind, existingResource.GetName())
	return op.Updated, existingResource, nil
}
