package kongroute

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// RoutesForRule creates or updates one KongRoute per HTTPRouteMatch in the given rule.
//
// Gateway API semantics are:
// - Within a single HTTPRouteRule, entries in .Matches are ORed
// - Within a single HTTPRouteMatch, individual criteria (path/method/headers) are ANDed
//
// To faithfully represent this in Kong, we generate one KongRoute for each HTTPRouteMatch
// and attach only that match's criteria to the route. All routes point to the same KongService.
// This fixes Hybrid Gateway conformance failures such as HTTPRouteMatching, which includes
// rules that combine independent path-only and header-only matches.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongRoutes are derived
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the KongRoutes
//   - serviceName: The name of the KongService these KongRoutes should point to
//   - hostnames: The hostnames for the KongRoutes
//
// Returns:
//   - kongRoutes: The created or updated KongRoute resources (one per match)
//   - err: Any error that occurred during the process
func RoutesForRule[
	T gwtypes.SupportedRoute,
	R gwtypes.SupportedRouteRule,
](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	route T,
	rule R,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	serviceName string,
	hostnames []string,
) (kongRoutes []*configurationv1alpha1.KongRoute, err error) {
	routeName := namegen.NewKongRouteName(route, cp, rule)
	routeBuilder := builder.NewKongRoute().
		WithName(routeName).
		WithNamespace(metadata.NamespaceFromParentRef(route, pRef)).
		WithLabels(route, pRef).
		WithAnnotations(route, pRef).
		WithSpecName(routeName).
		WithKongService(serviceName)

	switch r := any(route).(type) {
	case *gwtypes.HTTPRoute:
		httpRule, ok := any(rule).(gwtypes.HTTPRouteRule)
		if !ok {
			return nil, fmt.Errorf("rule type %T and route type %T does not match", rule, route)
		}
		// If the rule has no matches, create a single catch-all route.
		// Kong requires at least one matcher; use "/" path to represent catch-all.
		if len(httpRule.Matches) == 0 {
			match := gatewayv1.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchPathPrefix), Value: new("/")},
			}
			httpRule.Matches = append(httpRule.Matches, match)
		}

		// Check filters to determine if we need capture groups in paths.
		setCaptureGroup := needsCaptureGroup(httpRule)

		for i, match := range httpRule.Matches {
			matchRouteName := namegen.NewKongRouteNameForMatch(r, cp, match, i)
			mLog := logger.WithValues("kongroute", matchRouteName, "matchIndex", i)
			log.Debug(mLog, "Creating KongRoute for HTTPRoute match")

			matchRouteBuilder := routeBuilder.Clone().
				WithName(routeName).
				WithHosts(hostnames).
				WithStripPath(metadata.ExtractStripPath(r.Annotations)).
				WithPreserveHost(metadata.ExtractPreserveHost(r.Annotations)).
				WithHTTPRouteMatch(match, setCaptureGroup)

			newRoute, buildErr := matchRouteBuilder.Build()
			if buildErr != nil {
				log.Error(mLog, buildErr, "Failed to build KongRoute resource")
				return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, buildErr)
			}

			if _, updErr := translator.VerifyAndUpdate(ctx, mLog, cl, &newRoute, r, true); updErr != nil {
				return nil, updErr
			}

			// Add to result slice as an explicit copy for clarity.
			// Using DeepCopy expresses the intent that each match yields an
			// independent KongRoute object.
			kongRoutes = append(kongRoutes, newRoute.DeepCopy())
		}
	case *gwtypes.TLSRoute:
		routeBuilder.WithSNI(hostnames)

		newRoute, buildErr := routeBuilder.Build()
		if buildErr != nil {
			log.Error(logger, buildErr, "Failed to build KongRoute resource")
			return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, buildErr)
		}
		kongRoutes = append(kongRoutes, &newRoute)

		if _, updErr := translator.VerifyAndUpdate(ctx, logger, cl, &newRoute, r, true); updErr != nil {
			return nil, updErr
		}
	}

	return kongRoutes, nil
}

// needsCaptureGroup checks if the given HTTPRoute rule requires a capture group
// in the KongRoute paths based on the presence of URLRewrite or RequestRedirect
// filters with ReplacePrefixMatch.
func needsCaptureGroup(rule gwtypes.HTTPRouteRule) bool {
	for _, filter := range rule.Filters {
		switch {
		case filter.Type == gatewayv1.HTTPRouteFilterURLRewrite &&
			filter.URLRewrite != nil &&
			filter.URLRewrite.Path != nil &&
			filter.URLRewrite.Path.ReplacePrefixMatch != nil:
			return true
		case filter.Type == gatewayv1.HTTPRouteFilterRequestRedirect &&
			filter.RequestRedirect != nil &&
			filter.RequestRedirect.Path != nil &&
			filter.RequestRedirect.Path.ReplacePrefixMatch != nil:
			return true
		}
	}
	return false
}
