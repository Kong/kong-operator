package upstream

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
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
	var hostHeader string
	var hostHeaderSet bool

	switch r := any(parentRoute).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongUpstream: unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		upstreamName = namegen.NewKongUpstreamNameForHTTPRouteRule(r, cp, httpRule)
		backendRefs := lo.Map(httpRule.BackendRefs, func(ref gwtypes.HTTPBackendRef, _ int) gwtypes.BackendRef { return ref.BackendRef })
		hostHeader, hostHeaderSet = resolveHostHeaderFromBackendRefs(ctx, cl, r.Namespace, backendRefs, logger)
	case *gwtypes.TLSRoute:
		tlsRule, ok := any(rule).(gwtypes.TLSRouteRule)
		if !ok {
			return nil, fmt.Errorf("failed to build KongUpstream: unmatched route type and rule type: %T and %T", parentRoute, rule)
		}
		upstreamName = namegen.NewKongUpstreamNameForTLSRouteRule(r, cp, tlsRule)
		hostHeader, hostHeaderSet = resolveHostHeaderFromBackendRefs(ctx, cl, r.Namespace, tlsRule.BackendRefs, logger)
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
		WithHostHeader(hostHeader, hostHeaderSet).
		WithControlPlaneRef(*cp).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongUpstream resource")
		return nil, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &upstream, parentRoute, false); err != nil {
		return nil, err
	}

	return &upstream, nil
}

// resolveHostHeaderFromBackendRefs returns the first host-header value found on any of the
// provided backend Services. Works for HTTPRoute, TLSRoute, and any future route kind — callers
// are responsible for converting their rule's BackendRefs to []gwtypes.BackendRef.
func resolveHostHeaderFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) (string, bool) {
	for _, backendRef := range backendRefs {
		if v, ok := extractHostHeaderFromBackendRef(ctx, cl, logger, namespace, backendRef); ok {
			return v, true
		}
	}
	return "", false
}

// extractHostHeaderFromBackendRef returns the konghq.com/host-header annotation value from the
// backend Service referenced by backendRef.
func extractHostHeaderFromBackendRef(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	namespace string,
	backendRef gwtypes.BackendRef,
) (string, bool) {
	if !route.IsBackendRefSupported(backendRef.Group, backendRef.Kind) {
		return "", false
	}

	bRefNamespace := namespace
	if backendRef.Namespace != nil && *backendRef.Namespace != "" {
		bRefNamespace = string(*backendRef.Namespace)
	}

	svc := &corev1.Service{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: bRefNamespace, Name: string(backendRef.Name)}, svc); err != nil {
		log.Debug(logger, "Failed to fetch backend Service for host-header annotation check",
			"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "error", err)
		return "", false
	}

	v, ok := metadata.ExtractHostHeader(svc.GetAnnotations())
	if !ok {
		return "", false
	}

	log.Debug(logger, "Using host-header from backend Service annotation",
		"service", fmt.Sprintf("%s/%s", bRefNamespace, backendRef.Name), "host-header", v)
	return v, true
}
