package kongroute

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
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// RouteForRule creates or updates a KongRoute for the given HTTPRoute rule.
// This function handles the creation of routes with proper annotations that track
// which HTTPRoutes reference the route. If a route already exists, it appends
// the current HTTPRoute name to the existing annotation instead of overwriting it.
//
// The function performs the following operations:
// 1. Generates the route name using the namegen package
// 2. Checks if a route with that name already exists in the cluster
// 3. If it exists, merges the current HTTPRoute into the existing annotations
// 4. If it doesn't exist, creates a new route with the current HTTPRoute in annotations
// 5. Returns the route resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource that needs the route
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the route
//   - serviceName: The name of the service this route should point to
//   - hostnames: The hostnames for the route
//
// Returns:
//   - *configurationv1alpha1.KongRoute: The created or updated route resource
//   - error: Any error that occurred during the process
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
) (*configurationv1alpha1.KongRoute, error) {
	routeName := namegen.NewKongRouteName(httpRoute, cp, rule)
	logger = logger.WithValues("kongroute", routeName)
	log.Debug(logger, "Creating KongRoute for HTTPRoute rule")

	// Check if the route already exists
	existingRoute := &configurationv1alpha1.KongRoute{}
	namespacedName := types.NamespacedName{
		Name:      routeName,
		Namespace: httpRoute.Namespace,
	}

	log.Debug(logger, "Creating a new KongRoute resource")

	routeBuilder := builder.NewKongRoute().
		WithName(routeName).
		WithNamespace(httpRoute.Namespace).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithSpecName(routeName).
		WithHosts(hostnames).
		WithStripPath(metadata.ExtractStripPath(httpRoute.Annotations)).
		WithKongService(serviceName).
		WithOwner(httpRoute)

	// Add HTTPRoute matches
	for _, match := range rule.Matches {
		routeBuilder = routeBuilder.WithHTTPRouteMatch(match)
	}
	newRoute, err := routeBuilder.Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongRoute resource")
		return nil, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err)
	}

	err = cl.Get(ctx, namespacedName, existingRoute)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongRoute")
		return nil, fmt.Errorf("failed to check for existing KongRoute %s: %w", routeName, err)
	}

	// Route doesn't exist yet
	if apierrors.IsNotFound(err) {
		log.Debug(logger, "Successfully created new KongRoute")
		return &newRoute, nil
	}

	// Route is already there, check it is managed by this HTTPRoute
	annotationManager := annotations.NewAnnotationManager(logger)
	if !annotationManager.ContainsHTTPRoute(existingRoute, httpRoute) {
		err := fmt.Errorf("KongRoute %s already exists and is managed by another HTTPRoute", routeName)
		log.Error(logger, err, "Failed to create/update KongRoute resource, skipping rule")
		return nil, err
	}

	log.Debug(logger, "Successfully updated existing KongRoute")

	return &newRoute, nil
}
