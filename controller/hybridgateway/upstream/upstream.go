package upstream

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// UpstreamForRule creates or updates a KongUpstream for the given route rule.
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
//   - parentRoute: The route resource from which the KongUpstream is derived
//   - rule: The specific rule within the route
//   - pRef: The parent reference (Gateway) for the route
//   - cp: The control plane reference for the KongUpstream
//
// Returns:
//   - kongUpstream: The translated KongUpstream resource
//   - err: Any error that occurred during the process
func UpstreamForRule[
	T gwtypes.SupportedRoute,
	TPtr gwtypes.SupportedRoutePtr[T],
	R gwtypes.SupportedRouteRule,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	parentRoute TPtr,
	rule R,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
) (kongUpstream *configurationv1alpha1.KongUpstream, err error) {

	var upstreamName string
	var policy *configurationv1beta1.KongUpstreamPolicy

	switch r := any(parentRoute).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongUpstream: unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		upstreamName = namegen.NewKongUpstreamNameForHTTPRouteRule(r, cp, httpRule)
		policy = upstreamPolicyForRouteRule(ctx, logger, cl, parentRoute.GetNamespace(), httpRule)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongUpstream: unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		upstreamName = namegen.NewKongUpstreamNameForTLSRouteRule(r, cp, tlsRule)
		policy = upstreamPolicyForRouteRule(ctx, logger, cl, parentRoute.GetNamespace(), tlsRule)
	// TODO: add other types of rules when we support them.

	// Should be unreachable.
	default:
		return nil, fmt.Errorf("failed to build KongUpstream: unsupported route type: %T", parentRoute)
	}
	logger = logger.WithValues("kongupstream", upstreamName)
	log.Debug(logger, fmt.Sprintf("Creating KongUpstream for %s rule", parentRoute.GetObjectKind().GroupVersionKind().Kind))

	upstream, err := builder.NewKongUpstream().
		WithName(upstreamName).
		WithNamespace(metadata.NamespaceFromParentRef(parentRoute, pRef)).
		WithLabels(parentRoute, pRef).
		WithAnnotations(parentRoute, pRef).
		WithSpecName(upstreamName).
		WithControlPlaneRef(*cp).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongUpstream resource")
		return nil, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err)
	}

	applyPolicyToUpstream(&upstream, policy)

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &upstream, parentRoute, false); err != nil {
		return nil, err
	}

	return &upstream, nil
}
