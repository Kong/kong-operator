package metadata

import (
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

const (
	// Annotation constants matching those in the ingress controller
	annotationPrefix = "konghq.com"
	stripPathKey     = "/strip-path"
)

// ExtractStripPath extracts the strip-path annotation value and returns a boolean.
// Returns true by default if the annotation is not present or cannot be parsed.
func ExtractStripPath(anns map[string]string) bool {
	if anns == nil {
		return true
	}

	val := anns[annotationPrefix+stripPathKey]
	if val == "" {
		return true // Default to true when not specified
	}

	stripPath, err := strconv.ParseBool(val)
	if err != nil {
		return true // Default to true when invalid value
	}

	return stripPath
}

// BuildAnnotations creates the standard annotations map for Kong resources managed by HTTPRoute.
func BuildAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) map[string]string {
	gwObjKey := client.ObjectKey{
		Name: string(parentRef.Name),
	}
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		gwObjKey.Namespace = string(*parentRef.Namespace)
	} else {
		gwObjKey.Namespace = route.GetNamespace()
	}

	return map[string]string{
		consts.GatewayOperatorHybridRoutesAnnotation:   client.ObjectKeyFromObject(route).String(),
		consts.GatewayOperatorHybridGatewaysAnnotation: gwObjKey.String(),
	}
}

// AnnotationManager provides comma-separated annotations that track Route references on Kubernetes objects.
// It allows logging of annotation updates.
type AnnotationManager struct {
	logger logr.Logger
}

// NewAnnotationManager creates a new AnnotationManager instance.
// It requires a logger for logging annotation operations.
func NewAnnotationManager(logger logr.Logger) *AnnotationManager {
	return &AnnotationManager{
		logger: logger,
	}
}

// AppendRouteToAnnotation appends the given route to the hybrid-routes annotation.
// The hybrid-routes annotation format is: "namespace/name,namespace2/name2,..."
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object (has GetAnnotations/SetAnnotations)
//   - route: The Route to append to the hybrid-routes annotation
//
// Returns:
//   - bool: true if the hybrid-routes annotation was modified, false if the annotation was already
//     there and no changes were made
func (am *AnnotationManager) AppendRouteToAnnotation(obj metav1.Object, route client.Object) bool {
	currentRouteKey := client.ObjectKeyFromObject(route).String()
	currentRouteAnnotation := currentRouteKey

	log.Debug(am.logger, "Processing route annotation",
		"currentRoute", currentRouteAnnotation,
		"objectName", obj.GetName(),
		"objectNamespace", obj.GetNamespace())

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Get existing hybrid-routes annotation
	hybridRouteAnnotation, exists := annotations[consts.GatewayOperatorHybridRoutesAnnotation]

	if !exists || hybridRouteAnnotation == "" {
		// No existing hybrid-routes annotation, set it to the current route
		annotations[consts.GatewayOperatorHybridRoutesAnnotation] = currentRouteAnnotation
		obj.SetAnnotations(annotations)
		log.Debug(am.logger, "Set new hybrid-routes annotation",
			"annotation", currentRouteAnnotation,
			"objectName", obj.GetName())
		return true
	}

	for route := range strings.SplitSeq(hybridRouteAnnotation, ",") {
		route = strings.TrimSpace(route)
		if route == currentRouteAnnotation {
			log.Debug(am.logger, "Route already exists in annotation, no update needed",
				"currentRoute", currentRouteAnnotation,
				"objectName", obj.GetName())
			return false
		}
	}

	// Append current route to existing list
	updatedAnnotation := hybridRouteAnnotation + "," + currentRouteAnnotation
	annotations[consts.GatewayOperatorHybridRoutesAnnotation] = updatedAnnotation
	obj.SetAnnotations(annotations)

	log.Debug(am.logger, "Appended Route to existing annotation",
		"previousAnnotation", hybridRouteAnnotation,
		"updatedAnnotation", updatedAnnotation,
		"objectName", obj.GetName())

	return true
}

// RemoveRouteFromAnnotation removes the given route from the hybrid-routes annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - route: The Route to remove from the hybrid-routes annotation
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
func (am *AnnotationManager) RemoveRouteFromAnnotation(obj metav1.Object, route client.Object) bool {
	currentRouteKey := client.ObjectKeyFromObject(route).String()
	currentRouteAnnotation := currentRouteKey

	log.Debug(am.logger, "Removing route from annotation",
		"routeToRemove", currentRouteAnnotation,
		"objectName", obj.GetName())

	annotations := obj.GetAnnotations()
	if annotations == nil {
		log.Debug(am.logger, "No annotations present, nothing to remove",
			"routeToRemove", currentRouteAnnotation,
			"objectName", obj.GetName())
		return false
	}

	// Get existing hybrid-routes annotation
	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRoutesAnnotation]

	if !exists || existingAnnotation == "" {
		log.Debug(am.logger, "No hybrid-routes annotation exists, nothing to remove",
			"routeToRemove", currentRouteAnnotation,
			"objectName", obj.GetName())
		return false
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
		log.Debug(am.logger, "Route not found in annotation, no changes made",
			"routeToRemove", currentRouteAnnotation,
			"objectName", obj.GetName())
		return false
	}

	// Update annotation
	if len(remainingRoutes) == 0 {
		// No routes left, remove the annotation entirely
		delete(annotations, consts.GatewayOperatorHybridRoutesAnnotation)
		log.Debug(am.logger, "Removed hybrid-routes annotation completely as no routes remain",
			"objectName", obj.GetName())
	} else {
		// Update with remaining routes
		updatedAnnotation := strings.Join(remainingRoutes, ",")
		annotations[consts.GatewayOperatorHybridRoutesAnnotation] = updatedAnnotation
		log.Debug(am.logger, "Updated hybrid-routes annotation",
			"previousAnnotation", existingAnnotation,
			"updatedAnnotation", updatedAnnotation,
			"objectName", obj.GetName())
	}
	obj.SetAnnotations(annotations)
	return true
}

// ContainsRoute checks if the given Route is present in the hybrid-routes annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - route: The route to check for in the annotations
//
// Returns:
//   - bool: true if the route is found in the annotation, false otherwise
func (am *AnnotationManager) ContainsRoute(obj metav1.Object, route client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	currentRouteKey := client.ObjectKeyFromObject(route).String()
	currentRouteAnnotation := currentRouteKey

	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	if !exists || existingAnnotation == "" {
		return false
	}

	for route := range strings.SplitSeq(existingAnnotation, ",") {
		route = strings.TrimSpace(route)
		if route == currentRouteAnnotation {
			return true
		}
	}

	return false
}

// GetRoutes returns all Route references from the hybrid-routes annotation
// of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//
// Returns:
//   - []string: List of route references in "namespace/name" format
func (am *AnnotationManager) GetRoutes(obj metav1.Object) []string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return []string{}
	}

	existingAnnotation, exists := annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	if !exists || existingAnnotation == "" {
		return []string{}
	}

	// Parse existing routes from the annotation
	var routes []string

	for route := range strings.SplitSeq(existingAnnotation, ",") {
		route = strings.TrimSpace(route)
		if route == "" {
			continue
		}

		// Format is now just "namespace/name"
		routes = append(routes, route)
	}

	return routes
}

// SetRoutes sets the hybrid-routes annotation to contain exactly the provided route references.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - routes: List of route references to set in the annotation
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
func (am *AnnotationManager) SetRoutes(obj metav1.Object, routes []string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}

	newAnnotation := strings.Join(routes, ",")

	// Check if annotation needs to be updated
	existingAnnotation := annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	existingRoutes := strings.Split(existingAnnotation, ",")
	if len(existingRoutes) == len(routes) {
		same := true
		for _, er := range existingRoutes {
			er = strings.TrimSpace(er)
			found := false
			for _, r := range routes {
				r = strings.TrimSpace(r)
				if er == r {
					found = true
					break
				}
			}
			if !found {
				same = false
				break
			}
		}
		if same {
			log.Debug(am.logger, "Hybrid-routes annotation already up to date",
				"objectName", obj.GetName())
			return false
		}
	}

	if newAnnotation == "" {
		delete(annotations, consts.GatewayOperatorHybridRoutesAnnotation)
	} else {
		annotations[consts.GatewayOperatorHybridRoutesAnnotation] = newAnnotation
	}
	obj.SetAnnotations(annotations)
	log.Debug(am.logger, "Set hybrid-routes annotation",
		consts.GatewayOperatorHybridRoutesAnnotation, newAnnotation,
		"objectName", obj.GetName())
	return true
}
