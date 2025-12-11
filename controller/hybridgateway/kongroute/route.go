package kongroute

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
//   - exists: A boolean indicating whether the KongRoute already exists (true) or must be created (false)
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
) (kongRoute *configurationv1alpha1.KongRoute, exists bool, err error) {
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
		WithKongService(serviceName)

	// Add HTTPRoute matches
	for _, match := range rule.Matches {
		routeBuilder = routeBuilder.WithHTTPRouteMatch(match)
	}
	newRoute, err := routeBuilder.Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongRoute resource")
		return nil, false, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err)
	}

	exists, err = translator.VerifyAndUpdate(ctx, logger, cl, &newRoute, httpRoute, true)
	if err != nil {
		return nil, false, err
	}

	return &newRoute, exists, nil
}
