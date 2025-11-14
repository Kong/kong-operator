package upstream

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
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
	logger = logger.WithValues("httpRoute", client.ObjectKeyFromObject(httpRoute))
	log.Debug(logger, "Creating upstream for HTTPRoute rule")

	upstreamName := namegen.NewKongUpstreamName(cp, rule)

	// Check if KongUpstream already exists
	existingUpstream := &configurationv1alpha1.KongUpstream{}
	upstreamKey := types.NamespacedName{
		Name:      upstreamName,
		Namespace: httpRoute.Namespace,
	}

	err := cl.Get(ctx, upstreamKey, existingUpstream)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongUpstream", "upstreamName", upstreamName)
		return nil, fmt.Errorf("failed to check for existing KongUpstream %s: %w", upstreamName, err)
	}

	var upstream *configurationv1alpha1.KongUpstream

	if apierrors.IsNotFound(err) {
		// KongUpstream doesn't exist, create a new one
		log.Debug(logger, "KongUpstream does not exist, creating new one", "upstreamName", upstreamName)

		newUpstream, err := builder.NewKongUpstream().
			WithName(upstreamName).
			WithNamespace(httpRoute.Namespace).
			WithLabels(httpRoute, pRef).
			WithAnnotations(httpRoute, pRef).
			WithSpecName(upstreamName).
			WithControlPlaneRef(*cp).
			Build()
		if err != nil {
			log.Error(logger, err, "Failed to build new KongUpstream", "upstreamName", upstreamName)
			return nil, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err)
		}

		upstream = &newUpstream
		log.Debug(logger, "Successfully created new KongUpstream", "upstreamName", upstreamName)
		return upstream, nil
	}

	// KongUpstream exists, update annotations to include current HTTPRoute
	log.Debug(logger, "KongUpstream found, updating resource", "upstreamName", upstreamName)

	// TODO: we should check that the exitstingUpstream.Spec matches what we expect and error out if not
	updatedUpstream, err := appendHTTPRouteToAnnotations(logger, existingUpstream, httpRoute)
	if err != nil {
		log.Error(logger, err, "Failed to append HTTPRoute to existing KongUpstream annotations", "upstreamName", upstreamName)
		return nil, fmt.Errorf("failed to append HTTPRoute to KongUpstream %s annotations: %w", upstreamName, err)
	}

	upstream = updatedUpstream
	log.Debug(logger, "KongUpstream updated", "upstreamName", upstreamName)

	return upstream, nil
}

// appendHTTPRouteToAnnotations takes an existing KongUpstream and appends the given HTTPRoute
// to the hybrid-route annotation if it's not already present.
//
// The hybrid-route annotation format is: "HTTPRoute|namespace/name,HTTPRoute|namespace2/name2,..."
// This function parses the existing annotation, checks if the current HTTPRoute is already listed,
// and if not, appends it to the list.
//
// Parameters:
//   - logger: Logger for debugging information
//   - upstream: The existing KongUpstream resource to update
//   - httpRoute: The HTTPRoute to add to the annotations
//
// Returns:
//   - *configurationv1alpha1.KongUpstream: The updated upstream with merged annotations
//   - error: Any error that occurred during processing
func appendHTTPRouteToAnnotations(
	logger logr.Logger,
	upstream *configurationv1alpha1.KongUpstream,
	httpRoute *gwtypes.HTTPRoute,
) (*configurationv1alpha1.KongUpstream, error) {
	if upstream.Annotations == nil {
		upstream.Annotations = make(map[string]string)
	}

	currentRouteKey := client.ObjectKeyFromObject(httpRoute).String()
	currentRouteAnnotation := "HTTPRoute|" + currentRouteKey

	log.Debug(logger, "Processing route annotation", "currentRoute", currentRouteAnnotation)

	// Get existing HTTPRoute annotations
	existingAnnotation, exists := upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation]

	if !exists || existingAnnotation == "" {
		// If the KongUpstream is there but with no annotations something unexpected happened.
		// Set the annotation to the current route.
		// TODO: we should probably return an error here instead.
		upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation] = currentRouteAnnotation
		log.Debug(logger, "Set new hybrid-route annotation", "annotation", currentRouteAnnotation)
		return upstream, nil
	}

	// Parse existing routes from the annotation
	existingRoutes := strings.Split(existingAnnotation, ",")

	// Check if current route is already in the list
	for _, route := range existingRoutes {
		route = strings.TrimSpace(route)
		if route == currentRouteAnnotation {
			log.Debug(logger, "HTTPRoute already exists in annotation, no update needed",
				"currentRoute", currentRouteAnnotation)
			return upstream, nil
		}
	}

	// Append current route to existing list
	updatedAnnotation := existingAnnotation + "," + currentRouteAnnotation
	upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation] = updatedAnnotation

	log.Debug(logger, "Appended HTTPRoute to existing annotation",
		"previousAnnotation", existingAnnotation,
		"updatedAnnotation", updatedAnnotation)

	return upstream, nil
}

// RemoveHTTPRouteFromAnnotations removes the given HTTPRoute from the hybrid-route annotation
// of the KongUpstream. This is useful for cleanup when an HTTPRoute is deleted or no longer
// references the upstream.
//
// Parameters:
//   - logger: Logger for debugging information
//   - upstream: The KongUpstream resource to update
//   - httpRoute: The HTTPRoute to remove from the annotations
//
// Returns:
//   - *configurationv1alpha1.KongUpstream: The updated upstream with the route removed
//   - bool: true if the annotation was modified, false if no changes were made
//   - error: Any error that occurred during processing
func RemoveHTTPRouteFromAnnotations(
	logger logr.Logger,
	upstream *configurationv1alpha1.KongUpstream,
	httpRoute *gwtypes.HTTPRoute,
) (*configurationv1alpha1.KongUpstream, bool, error) {
	if upstream.Annotations == nil {
		log.Debug(logger, "No annotations present, nothing to remove")
		return upstream, false, nil
	}

	currentRouteKey := client.ObjectKeyFromObject(httpRoute).String()
	currentRouteAnnotation := "HTTPRoute|" + currentRouteKey

	log.Debug(logger, "Removing route from annotation", "routeToRemove", currentRouteAnnotation)

	// Get existing hybrid-route annotation
	existingAnnotation, exists := upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation]

	if !exists || existingAnnotation == "" {
		log.Debug(logger, "No hybrid-route annotation exists, nothing to remove")
		return upstream, false, nil
	}

	// Parse existing routes from the annotation
	existingRoutes := strings.Split(existingAnnotation, ",")
	var remainingRoutes []string

	// Filter out the route to remove
	found := false
	for _, route := range existingRoutes {
		route = strings.TrimSpace(route)
		if route != currentRouteAnnotation {
			remainingRoutes = append(remainingRoutes, route)
		} else {
			found = true
		}
	}

	if !found {
		log.Debug(logger, "HTTPRoute not found in annotation, no changes made",
			"routeToRemove", currentRouteAnnotation)
		return upstream, false, nil
	}

	// Update annotation with remaining routes
	if len(remainingRoutes) == 0 {
		// No routes left, remove the annotation entirely
		delete(upstream.Annotations, consts.GatewayOperatorHybridRouteAnnotation)
		log.Debug(logger, "Removed hybrid-route annotation completely as no routes remain")
	} else {
		// Update with remaining routes
		updatedAnnotation := strings.Join(remainingRoutes, ",")
		upstream.Annotations[consts.GatewayOperatorHybridRouteAnnotation] = updatedAnnotation
		log.Debug(logger, "Updated hybrid-route annotation",
			"previousAnnotation", existingAnnotation,
			"updatedAnnotation", updatedAnnotation)
	}

	return upstream, true, nil
}
