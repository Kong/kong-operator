package metadata

import (
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	// Annotation constants matching those in the ingress controller.
	annotationPrefix = "konghq.com"
	stripPathKey     = "/strip-path"
	preserveHostKey  = "/preserve-host"
	protocolKey      = "/protocol"
	pathKey          = "/path"
	tlsVerifyKey     = "/tls-verify"
	tlsVerifyDepthKey = "/tls-verify-depth"
	kindHTTPRoute    = "HTTPRoute"
	kindTLSRoute     = "TLSRoute"
)

// Defaults for the annotations when not specified that match the behavior of on-prem.
const (
	// defaultStripPath is the default value for the strip-path annotation when not specified.
	defaultStripPath = false

	// defaultPreserveHost is the default value for the preserve-host annotation when not specified.
	defaultPreserveHost = true
)

// ExtractStripPath extracts the strip-path annotation value and returns a boolean.
// Returns false by default if the annotation is not present or cannot be parsed.
func ExtractStripPath(anns map[string]string) bool {
	parseStripPath, ok := parseAnnotationBool(anns, stripPathKey)
	if !ok {
		return defaultStripPath
	}
	return parseStripPath
}

// ExtractPreserveHost extracts the preserve-host annotation value and returns a boolean.
// Returns true by default if the annotation is not present or cannot be parsed.
func ExtractPreserveHost(anns map[string]string) bool {
	parsePreserveHost, ok := parseAnnotationBool(anns, preserveHostKey)
	if !ok {
		return defaultPreserveHost
	}
	return parsePreserveHost
}

// ExtractProtocol extracts the protocol supplied in the konghq.com/protocol annotation.
// Returns an empty string if the annotation is not present.
// This mirrors ingress-controller/internal/annotations.ExtractProtocolName.
func ExtractProtocol(anns map[string]string) string {
	return anns[annotationPrefix+protocolKey]
}

// ExtractPath extracts the konghq.com/path annotation value.
// Returns an empty string if the annotation is not present.
// This mirrors ingress-controller/internal/annotations.ExtractPath.
func ExtractPath(anns map[string]string) string {
	return anns[annotationPrefix+pathKey]
}

// ExtractTLSVerify extracts the tls-verify annotation value.
// Returns a *bool set to the parsed value when the annotation is present and parseable,
// or nil when absent or unparseable.
// This mirrors ingress-controller/internal/annotations.ExtractTLSVerify.
func ExtractTLSVerify(anns map[string]string) *bool {
	v, ok := parseAnnotationBool(anns, tlsVerifyKey)
	if !ok {
		return nil
	}
	return &v
}

// ExtractTLSVerifyDepth extracts the tls-verify-depth annotation value.
// Returns a *int64 set to the parsed value when the annotation is present and parseable as a
// non-negative integer, or nil when absent or unparseable.
// This mirrors ingress-controller/internal/annotations.ExtractTLSVerifyDepth.
func ExtractTLSVerifyDepth(anns map[string]string) *int64 {
	if anns == nil {
		return nil
	}
	val, ok := anns[annotationPrefix+tlsVerifyDepthKey]
	if !ok || val == "" {
		return nil
	}
	depth, err := strconv.ParseInt(val, 10, 64)
	if err != nil || depth < 0 {
		return nil
	}
	return &depth
}

// IsValidProtocol returns true if the provided protocol is a valid Kong upstream protocol.
// This mirrors ingress-controller/internal/util.ValidateProtocol.
func IsValidProtocol(protocol string) bool {
	switch protocol {
	case "http", "https", "grpc", "grpcs", "ws", "wss", "tls", "tcp", "tls_passthrough":
		return true
	default:
		return false
	}
}

func parseAnnotationBool(anns map[string]string, key string) (enabled bool, ok bool) {
	if anns == nil {
		return false, false
	}

	val := anns[annotationPrefix+key]
	if val == "" {
		return false, false // Annotation not present.
	}

	parsedVal, err := strconv.ParseBool(val)
	if err != nil {
		return false, false // Invalid value.
	}

	return parsedVal, true
}

// BuildAnnotations creates the standard annotations map for Kong resources managed by Gateway API objects.
// For supported routes (HTTPRoute, TLSRoute), it includes both route and gateway annotations.
// For Gateway, it only includes the gateway annotation.
func BuildAnnotations(obj client.Object, parentRef *gwtypes.ParentReference) map[string]string {
	gwObjKey := client.ObjectKey{
		Name: string(parentRef.Name),
	}
	if parentRef.Namespace != nil && *parentRef.Namespace != "" {
		gwObjKey.Namespace = string(*parentRef.Namespace)
	} else {
		gwObjKey.Namespace = obj.GetNamespace()
	}

	annotations := map[string]string{
		consts.GatewayOperatorHybridGatewaysAnnotation: gwObjKey.String(),
	}

	// Add route annotation for TLSRoute and HTTPRoute objects.
	// Use different annotation keys for each type to distinguish the routes with same namespace/name but different kinds.
	switch obj.(type) {
	case *gwtypes.HTTPRoute:
		annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation] = client.ObjectKeyFromObject(obj).String()
	case *gwtypes.TLSRoute:
		annotations[consts.GatewayOperatorHybridRoutesTLSRouteAnnotation] = client.ObjectKeyFromObject(obj).String()
	}

	return annotations
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

// ObjectToNameString converts a Kubernetes object to its string representation in the format "namespace/name".
func ObjectToNameString(obj client.Object) string {
	return client.ObjectKeyFromObject(obj).String()
}

// NameStringToObjectKey converts a string in the format "namespace/name" to a client.ObjectKey.
func NameStringToObjectKey(s string) client.ObjectKey {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return client.ObjectKey{}
	}
	return client.ObjectKey{
		Namespace: parts[0],
		Name:      parts[1],
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
	currentRouteKind := route.GetObjectKind().GroupVersionKind().Kind
	currentRouteObjectKey := ObjectToNameString(route)

	log.Debug(am.logger, "Processing route annotation",
		"routeKind", currentRouteKind,
		"currentRoute", currentRouteObjectKey,
		"objectName", obj.GetName(),
		"objectNamespace", obj.GetNamespace())

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Get existing hybrid-routes annotation
	routeAnnotationKey := am.RouteAnnotationKeyForKind(currentRouteKind)
	if routeAnnotationKey == "" {
		log.Debug(am.logger, "Unsupported route kind for setting annotation", "routeKind", currentRouteKind)
		return false
	}
	hybridRouteAnnotation, exists := annotations[routeAnnotationKey]

	if !exists || hybridRouteAnnotation == "" {
		// No existing hybrid-routes annotation, set it to the current route
		annotations[routeAnnotationKey] = currentRouteObjectKey
		obj.SetAnnotations(annotations)
		log.Debug(am.logger, "Set new hybrid-routes annotation",
			"annotation", currentRouteObjectKey,
			"objectName", obj.GetName())
		return true
	}

	for routeAnnotation := range strings.SplitSeq(hybridRouteAnnotation, ",") {
		if RouteAnnotationMatch(routeAnnotation, route) {
			log.Debug(am.logger, "Route already exists in annotation, no update needed",
				"currentRoute", currentRouteObjectKey,
				"objectName", obj.GetName())
			return false
		}
	}

	// Append current route to existing list
	updatedAnnotation := hybridRouteAnnotation + "," + currentRouteObjectKey
	annotations[routeAnnotationKey] = updatedAnnotation
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
	currentRouteKind := route.GetObjectKind().GroupVersionKind().Kind
	currentRouteObjectKey := client.ObjectKeyFromObject(route).String()

	log.Debug(am.logger, "Removing route from annotation",
		"routeKind", currentRouteKind,
		"routeToRemove", currentRouteObjectKey,
		"objectName", obj.GetName())

	annotations := obj.GetAnnotations()
	if annotations == nil {
		log.Debug(am.logger, "No annotations present, nothing to remove",
			"routeKind", currentRouteKind,
			"routeToRemove", currentRouteObjectKey,
			"objectName", obj.GetName())
		return false
	}

	// Get existing hybrid-routes annotation fir the route kind.
	routeAnnotationKey := am.RouteAnnotationKeyForKind(currentRouteKind)
	if routeAnnotationKey == "" {
		return false
	}
	existingAnnotation, exists := annotations[routeAnnotationKey]

	if !exists || existingAnnotation == "" {
		log.Debug(am.logger, "No hybrid-routes annotation exists, nothing to remove",
			"routeKind", currentRouteKind,
			"routeToRemove", currentRouteObjectKey,
			"objectName", obj.GetName())
		return false
	}

	existingRoutes := strings.Split(existingAnnotation, ",")
	var remainingRoutes []string

	// Filter out the route to remove
	found := false
	for _, routeAnnotation := range existingRoutes {
		if RouteAnnotationMatch(routeAnnotation, route) {
			found = true
			continue
		}
		remainingRoutes = append(remainingRoutes, routeAnnotation)
	}

	if !found {
		log.Debug(am.logger, "Route not found in annotation, no changes made",
			"routeKind", currentRouteKind,
			"routeToRemove", currentRouteObjectKey,
			"objectName", obj.GetName())
		return false
	}

	// Update annotation
	if len(remainingRoutes) == 0 {
		// No routes left, remove the annotation entirely
		delete(annotations, routeAnnotationKey)
		log.Debug(am.logger, "Removed hybrid-routes annotation completely as no routes remain",
			"objectName", obj.GetName())
	} else {
		// Update with remaining routes
		updatedAnnotation := strings.Join(remainingRoutes, ",")
		annotations[routeAnnotationKey] = updatedAnnotation
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

	currentRouteKind := route.GetObjectKind().GroupVersionKind().Kind
	routeAnnotationKey := am.RouteAnnotationKeyForKind(currentRouteKind)
	if routeAnnotationKey == "" {
		return false
	}
	existingAnnotation, exists := annotations[routeAnnotationKey]
	if !exists || existingAnnotation == "" {
		return false
	}

	return lo.ContainsBy(strings.Split(existingAnnotation, ","), func(routeAnnotation string) bool {
		return RouteAnnotationMatch(routeAnnotation, route)
	})
}

// GetRoutesWithKind returns all Route references having the given route kind
// from the hybrid-routes annotation of the provided Kubernetes object.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - routeKind: The kind of the route reference
//
// Returns:
//   - []string: List of route references in "namespace/name" format
func (am *AnnotationManager) GetRoutesWithKind(obj metav1.Object, routeKind string) []string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return []string{}
	}

	routeAnnotationKey := am.RouteAnnotationKeyForKind(routeKind)
	if routeAnnotationKey == "" {
		return []string{}
	}
	existingAnnotation, exists := annotations[routeAnnotationKey]

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

// SetRoutesWithKind sets the hybrid-routes annotation to contain exactly the provided route references with given kind.
//
// Parameters:
//   - obj: Any Kubernetes object that implements metav1.Object
//   - routeKind: The kind of the route references
//   - routes: List of route references (namespace/name) to set in the annotation
//
// Returns:
//   - bool: true if the annotation was modified, false if no changes were made
func (am *AnnotationManager) SetRoutesWithKind(obj metav1.Object, routeKind string, routes []string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}

	routeAnnotationKey := am.RouteAnnotationKeyForKind(routeKind)
	if routeAnnotationKey == "" {
		return false
	}

	newAnnotation := strings.Join(routes, ",")

	// Check if annotation needs to be updated
	existingAnnotation := annotations[routeAnnotationKey]
	existingRoutes := strings.Split(existingAnnotation, ",")

	same := lo.ElementsMatchBy(existingRoutes, routes, strings.TrimSpace)

	if same {
		log.Debug(am.logger, "Hybrid-routes annotation already up to date",
			"objectName", obj.GetName())
		return false
	}

	if newAnnotation == "" {
		delete(annotations, routeAnnotationKey)
	} else {
		annotations[routeAnnotationKey] = newAnnotation
	}
	obj.SetAnnotations(annotations)
	log.Debug(am.logger, "Set hybrid-routes annotation",
		routeAnnotationKey, newAnnotation,
		"objectName", obj.GetName())
	return true
}

// RouteAnnotationKeyForKind returns the annotation key for the route kind to
// mark the namespace and name of the route with given kind that owns the object.
func (am *AnnotationManager) RouteAnnotationKeyForKind(routeKind string) string {
	switch routeKind {
	case kindHTTPRoute:
		return consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation
	case kindTLSRoute:
		return consts.GatewayOperatorHybridRoutesTLSRouteAnnotation
	}
	// Not supported route kind. Should be unreachable.
	return ""
}

// RouteAnnotationMatch returns true if the hybrid route annotation matches the given route by its namespace and name.
func RouteAnnotationMatch(routeAnnotation string, route client.Object) bool {
	routeObjectKey := client.ObjectKeyFromObject(route)
	return strings.TrimSpace(routeAnnotation) == routeObjectKey.String()
}
