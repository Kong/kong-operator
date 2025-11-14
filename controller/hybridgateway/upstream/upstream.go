package upstream

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/annotations"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// UpstreamForRule creates or updates a KongUpstream for the given HTTPRoute rule.
// This function handles the creation of upstreams with proper annotations that track
// which HTTPRoutes reference the upstream. If an upstream already exists, it appends
// the current HTTPRoute name to the existing annotation instead of overwriting it.
//
// The function performs the following operations:
// 1. Generates the upstream name using the namegen package
// 2. Checks if an upstream with that name already exists in the cluster
// 3. If it exists, merges the current HTTPRoute into the existing annotations
// 4. If it doesn't exist, creates a new upstream with the current HTTPRoute in annotations
// 5. Returns the upstream resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource that needs the upstream
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the upstream
//
// Returns:
//   - *configurationv1alpha1.KongUpstream: The created or updated upstream resource
//   - error: Any error that occurred during the process
func UpstreamForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
) (*configurationv1alpha1.KongUpstream, error) {
	upstreamName := namegen.NewKongUpstreamName(cp, rule)
	logger = logger.WithValues("kongupstream", upstreamName)
	log.Debug(logger, "Creating KongUpstream for HTTPRoute rule")

	upstream, err := builder.NewKongUpstream().
		WithName(upstreamName).
		WithNamespace(httpRoute.Namespace).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithSpecName(upstreamName).
		WithControlPlaneRef(*cp).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongUpstream resource")
		return nil, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err)
	}

	// Check if KongUpstream already exists
	existingUpstream := &configurationv1alpha1.KongUpstream{}
	upstreamKey := types.NamespacedName{
		Name:      upstreamName,
		Namespace: httpRoute.Namespace,
	}
	if err = cl.Get(ctx, upstreamKey, existingUpstream); err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongUpstream")
		return nil, fmt.Errorf("failed to check for existing KongUpstream %s: %w", upstreamName, err)
	}

	if apierrors.IsNotFound(err) {
		// KongUpstream doesn't exist, create a new one
		log.Debug(logger, "Creating a new KongUpstream resource")
		return &upstream, nil
	}

	// KongUpstream exists, update annotations to include current HTTPRoute
	log.Debug(logger, "KongUpstream found")
	upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation] = existingUpstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation]
	annotationManager := annotations.NewAnnotationManager(logger)
	_, err = annotationManager.AppendHTTPRouteToAnnotation(&upstream, httpRoute)
	if err != nil {
		log.Error(logger, err, "Failed to update HTTPRoute annotation on KongUpstream")
		return nil, fmt.Errorf("failed to update HTTPRoute annotation on KongUpstream %s: %w", upstreamName, err)
	}

	// TODO: we should check that the existingUpstream.Spec matches what we expect and error out if not
	log.Debug(logger, "Successfully updated existing KongUpstream")

	return &upstream, nil
}
