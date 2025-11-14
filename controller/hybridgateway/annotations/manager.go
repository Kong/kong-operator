package annotations

import (
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// AnnotationManager provides generic functionality for managing comma-separated
// annotations that track HTTPRoute references on Kubernetes objects.
type AnnotationManager struct {
	logger logr.Logger
}

// NewAnnotationManager creates a new AnnotationManager instance.
func NewAnnotationManager(logger logr.Logger) *AnnotationManager {
	return &AnnotationManager{
		logger: logger,
	}
}

// AppendHTTPRouteToAnnotation appends the given HTTPRoute to the hybrid-route annotation.
// The hybrid-route annotation format is: "namespace/name,namespace2/name2,..."
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object (has GetAnnotations/SetAnnotations)
//   - httpRoute: The HTTPRoute to add to the annotations
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
//   - error: Any error that occurred during processing
func (am *AnnotationManager) AppendHTTPRouteToAnnotation(obj metav1.Object, httpRoute *gwtypes.HTTPRoute) (bool, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}

	currentRouteKey := client.ObjectKeyFromObject(httpRoute).String()
	currentRouteAnnotation := currentRouteKey

	log.Debug(am.logger, "Processing route annotation",
		"currentRoute", currentRouteAnnotation,
		"objectName", obj.GetName(),
		"objectNamespace", obj.GetNamespace())

	// Get existing hybrid-route annotation
	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRouteAnnotation]

	if !exists || existingAnnotation == "" {
		// No existing annotation, set it to the current route
		annotations[consts.GatewayOperatorHybridRouteAnnotation] = currentRouteAnnotation
		log.Debug(am.logger, "Set new hybrid-route annotation",
			"annotation", currentRouteAnnotation,
			"objectName", obj.GetName())
		return true, nil
	}

	existingRoutes := strings.Split(existingAnnotation, ",")

	for _, route := range existingRoutes {
		route = strings.TrimSpace(route)
		if route == currentRouteAnnotation {
			log.Debug(am.logger, "HTTPRoute already exists in annotation, no update needed",
				"currentRoute", currentRouteAnnotation,
				"objectName", obj.GetName())
			return false, nil
		}
	}

	// Append current route to existing list
	updatedAnnotation := existingAnnotation + "," + currentRouteAnnotation
	annotations[consts.GatewayOperatorHybridRouteAnnotation] = updatedAnnotation

	log.Debug(am.logger, "Appended HTTPRoute to existing annotation",
		"previousAnnotation", existingAnnotation,
		"updatedAnnotation", updatedAnnotation,
		"objectName", obj.GetName())

	return true, nil
}

// RemoveHTTPRouteFromAnnotation removes the given HTTPRoute from the hybrid-route annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - httpRoute: The HTTPRoute to remove from the annotations
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
//   - error: Any error that occurred during processing
func (am *AnnotationManager) RemoveHTTPRouteFromAnnotation(obj metav1.Object, httpRoute *gwtypes.HTTPRoute) (bool, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		log.Debug(am.logger, "No annotations present, nothing to remove",
			"objectName", obj.GetName())
		return false, nil
	}

	currentRouteKey := client.ObjectKeyFromObject(httpRoute).String()
	currentRouteAnnotation := currentRouteKey

	log.Debug(am.logger, "Removing route from annotation",
		"routeToRemove", currentRouteAnnotation,
		"objectName", obj.GetName())

	// Get existing hybrid-route annotation
	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRouteAnnotation]

	if !exists || existingAnnotation == "" {
		log.Debug(am.logger, "No hybrid-route annotation exists, nothing to remove",
			"objectName", obj.GetName())
		return false, nil
	}

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
		log.Debug(am.logger, "HTTPRoute not found in annotation, no changes made",
			"routeToRemove", currentRouteAnnotation,
			"objectName", obj.GetName())
		return false, nil
	}

	// Update annotation
	if len(remainingRoutes) == 0 {
		// No routes left, remove the annotation entirely
		delete(annotations, consts.GatewayOperatorHybridRouteAnnotation)
		log.Debug(am.logger, "Removed hybrid-route annotation completely as no routes remain",
			"objectName", obj.GetName())
	} else {
		// Update with remaining routes
		updatedAnnotation := strings.Join(remainingRoutes, ",")
		annotations[consts.GatewayOperatorHybridRouteAnnotation] = updatedAnnotation
		log.Debug(am.logger, "Updated hybrid-route annotation",
			"previousAnnotation", existingAnnotation,
			"updatedAnnotation", updatedAnnotation,
			"objectName", obj.GetName())
	}

	return true, nil
}

// ContainsHTTPRoute checks if the given HTTPRoute is present in the hybrid-route annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - httpRoute: The HTTPRoute to check for in the annotations
//
// Returns:
//   - bool: true if the HTTPRoute is found in the annotation, false otherwise
func (am *AnnotationManager) ContainsHTTPRoute(obj metav1.Object, httpRoute *gwtypes.HTTPRoute) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	currentRouteKey := client.ObjectKeyFromObject(httpRoute).String()
	currentRouteAnnotation := currentRouteKey

	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRouteAnnotation]
	if !exists || existingAnnotation == "" {
		return false
	}

	existingRoutes := strings.Split(existingAnnotation, ",")

	for _, route := range existingRoutes {
		route = strings.TrimSpace(route)
		if route == currentRouteAnnotation {
			return true
		}
	}

	return false
}

// GetHTTPRoutes returns all HTTPRoute references from the hybrid-route annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//
// Returns:
//   - []string: List of HTTPRoute references in "namespace/name" format
//   - error: Any error that occurred during parsing
func (am *AnnotationManager) GetHTTPRoutes(obj metav1.Object) ([]string, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return []string{}, nil
	}

	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRouteAnnotation]
	if !exists || existingAnnotation == "" {
		return []string{}, nil
	}

	// Parse existing routes from the annotation
	existingRoutes := strings.Split(existingAnnotation, ",")
	var httpRoutes []string

	for _, route := range existingRoutes {
		route = strings.TrimSpace(route)
		if route == "" {
			continue
		}

		// Format is now just "namespace/name"
		httpRoutes = append(httpRoutes, route)
	}

	return httpRoutes, nil
}

// SetHTTPRoutes sets the hybrid-route annotation to contain exactly the provided HTTPRoute references.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - httpRoutes: List of HTTPRoute objects to set in the annotation
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
func (am *AnnotationManager) SetHTTPRoutes(obj metav1.Object, httpRoutes []*gwtypes.HTTPRoute) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}

	var routeAnnotations []string
	for _, httpRoute := range httpRoutes {
		routeKey := client.ObjectKeyFromObject(httpRoute).String()
		routeAnnotations = append(routeAnnotations, routeKey)
	}

	newAnnotation := strings.Join(routeAnnotations, ",")

	// Check if annotation needs to be updated
	existingAnnotation := annotations[consts.GatewayOperatorHybridRouteAnnotation]
	if existingAnnotation == newAnnotation {
		return false
	}

	if newAnnotation == "" {
		delete(annotations, consts.GatewayOperatorHybridRouteAnnotation)
	} else {
		annotations[consts.GatewayOperatorHybridRouteAnnotation] = newAnnotation
	}

	return true
}
