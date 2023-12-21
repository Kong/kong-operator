package controllers

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/utils/op"
)

// patchIfPatchIsNonEmpty patches the provided Deployment if the resulting patch
// between the provided existingDeployment and the provided oldExistingDeployment
// is non empty.
func patchIfPatchIsNonEmpty[OwnerT operatorv1beta1.DataPlane | operatorv1alpha1.ControlPlane](
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	existingDeployment *appsv1.Deployment,
	oldExistingDeployment *appsv1.Deployment,
	owner *OwnerT,
	updated bool,
) (res op.CreatedUpdatedOrNoop, deploy *appsv1.Deployment, err error) {
	if !updated {
		trace(log, "No need for Deployment update", owner, "deployment", existingDeployment.Name)
		return op.Noop, existingDeployment, nil
	}

	// Check if the patch to be applied is empty.
	patch := client.MergeFrom(oldExistingDeployment)
	b, err := patch.Data(existingDeployment)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate patch for Deployment %s: %w", existingDeployment.Name, err)
	}
	// Only perform the patch operation if the resulting patch is non empty.
	if len(b) == 0 || bytes.Equal(b, []byte("{}")) {
		trace(log, "No need for Deployment update", owner, "deployment", existingDeployment.Name)
		return op.Noop, existingDeployment, nil
	}

	if err := cl.Patch(ctx, existingDeployment, client.MergeFrom(oldExistingDeployment)); err != nil {
		return op.Noop, existingDeployment, fmt.Errorf("failed patching Deployment %s: %w", existingDeployment.Name, err)
	}
	debug(log, "Deployment modified", owner, "deployment", existingDeployment.Name)
	return op.Updated, existingDeployment, nil
}
