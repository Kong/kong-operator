package upstream

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// UpstreamForRule creates or updates a KongUpstream for the given HTTPRoute rule.
//
// The function performs the following operations:
// 1. Generates the KongUpstream name using the namegen package
// 2. Checks if a KongUpstream with that name already exists in the cluster
// 3. If it exists, updates the KongUpstream
// 4. If it doesn't exist, creates a new KongUpstream
// 5. Returns the KongUpstream resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongUpstream is derived
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the KongUpstream
//
// Returns:
//   - kongUpstream: The translated KongUpstream resource
//   - exists: A boolean indicating whether the KongUpstream already exists (true) or must be created (false)
//   - err: Any error that occurred during the process
func UpstreamForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
) (kongUpstream *configurationv1alpha1.KongUpstream, exists bool, err error) {
	upstreamName := namegen.NewKongUpstreamName(cp, rule)
	logger = logger.WithValues("kongupstream", upstreamName)
	log.Debug(logger, "Creating KongUpstream for HTTPRoute rule")

	upstream, err := builder.NewKongUpstream().
		WithName(upstreamName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithSpecName(upstreamName).
		WithControlPlaneRef(*cp).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongUpstream resource")
		return nil, false, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err)
	}

	exists, err = translator.VerifyAndUpdate(ctx, logger, cl, &upstream, httpRoute, false)
	if err != nil {
		return nil, false, err
	}

	return &upstream, exists, nil
}
