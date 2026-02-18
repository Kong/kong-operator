package kongroute

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
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

// RouteForRule creates or updates a KongRoute for the given HTTPRoute rule.
//
// The function performs the following operations:
// 1. Generates the KongRoute name using the namegen package
// 2. Checks if a KongRoute with that name already exists in the cluster
// 3. If it exists, updates the KongRoute
// 4. If it doesn't exist, creates a new KongRoute
// 5. Returns the KongRoute resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongRoute is derived
//   - rule: The specific rule within the HTTPRoute from which the KongRoute is derived
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the KongRoute
//   - serviceName: The name of the KongService this KongRoute should point to
//   - hostnames: The hostnames for the KongRoute
//
// Returns:
//   - kongRoute: The created or updated KongRoute resource
//   - err: Any error that occurred during the process
func RouteForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	serviceName string,
	hostnames []string,
) (kongRoute *configurationv1alpha1.KongRoute, err error) {
	routeName := namegen.NewKongRouteName(httpRoute, cp, rule)
	logger = logger.WithValues("kongroute", routeName)
	log.Debug(logger, "Creating KongRoute for HTTPRoute rule")

	routeBuilder := builder.NewKongRoute().
		WithName(routeName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithSpecName(routeName).
		WithHosts(hostnames).
		WithStripPath(metadata.ExtractStripPath(httpRoute.Annotations)).
		WithPreserveHost(metadata.ExtractPreserveHost(httpRoute.Annotations)).
		WithKongService(serviceName)

	// Check if the rule contains a URLRewrite or RequestRedirect filter with ReplacePrefixMatch:
	// if so, we need to set a capture group on the KongRoute paths.
	setCaptureGroup := needsCaptureGroup(rule)

	// Add HTTPRoute matches
	for _, match := range rule.Matches {
		routeBuilder = routeBuilder.WithHTTPRouteMatch(match, setCaptureGroup)
	}
	newRoute, err := routeBuilder.Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongRoute resource")
		return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &newRoute, httpRoute, true); err != nil {
		return nil, err
	}

	return &newRoute, nil
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
