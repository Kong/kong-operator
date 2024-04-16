package patch

import (
	"bytes"
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
)

// ApplyPatchIfNonEmpty patches the provided resource if the resulting patch
// between the provided existingResource and the provided oldExistingResource
// is non empty.
func ApplyPatchIfNonEmpty[
	OwnerT *operatorv1beta1.DataPlane | *operatorv1beta1.ControlPlane,
	ResourceT interface {
		*appsv1.Deployment | *autoscalingv2.HorizontalPodAutoscaler | *certmanagerv1.Certificate
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

// ApplyGatewayStatusPatchIfNotEmpty patches the provided gateways if the
// resulting patch between the provided existingGateway and oldExistingGateway
// is non empty.
func ApplyGatewayStatusPatchIfNotEmpty(ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	existingGateway *gatewayv1.Gateway,
	oldExistingGateway *gatewayv1.Gateway) (res op.CreatedUpdatedOrNoop, err error) {
	// Check if the patch to be applied is empty.
	patch := client.MergeFrom(oldExistingGateway)
	b, err := patch.Data(existingGateway)
	if err != nil {
		return op.Noop, fmt.Errorf("failed to generate patch for gateway %s/%s: %w", existingGateway.Namespace, existingGateway.Name, err)
	}
	// Only perform the patch operation if the resulting patch is non empty.
	if len(b) == 0 || bytes.Equal(b, []byte("{}")) {
		log.Trace(logger, "No need for update", existingGateway)
		return op.Noop, nil
	}

	if err := cl.Status().Patch(ctx, existingGateway, client.MergeFrom(oldExistingGateway)); err != nil {
		return op.Noop, fmt.Errorf("failed patching gateway %s/%s: %w", existingGateway.Namespace, existingGateway.Name, err)
	}
	log.Debug(logger, "Resource modified", existingGateway)
	return op.Updated, nil
}
