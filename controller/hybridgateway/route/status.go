package route

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

// SetStatusConditions updates the status conditions for a specific ParentReference managed by the given controller.
// It finds the ParentStatus entry that matches both the ParentReference and controllerName, then updates or adds
// the provided conditions. If no matching ParentStatus exists, it creates a new one.
//
// The function performs intelligent condition management:
// - Only updates conditions that have actually changed (ignoring LastTransitionTime)
// - Automatically sets LastTransitionTime when conditions are modified
// - Preserves existing conditions that are not being updated
// - Only manages ParentStatus entries owned by the specified controller
//
// Parameters:
//   - route: The HTTPRoute to update
//   - pRef: The ParentReference to match against
//   - controllerName: The controller name that should own the ParentStatus entry
//   - conditions: Variable number of conditions to set or update
//
// Returns:
//   - bool: true if any conditions were added or modified, false if no changes were made
//
// This function respects controller ownership and will not modify ParentStatus entries
// managed by other controllers, making it safe for use in multi-controller environments.
func SetStatusConditions(route *gwtypes.HTTPRoute, pRef gwtypes.ParentReference, controllerName string, conditions ...metav1.Condition) bool {
	updated := false

	// Find the ParentStatus for the given ParentRef that is managed by our controller.
	var targetParentStatus *gwtypes.RouteParentStatus
	for i := range route.Status.Parents {
		if string(route.Status.Parents[i].ControllerName) == controllerName &&
			isParentRefEqual(route.Status.Parents[i].ParentRef, pRef) {
			targetParentStatus = &route.Status.Parents[i]
			break
		}
	}

	// If ParentStatus doesn't exist for our controller, create it.
	if targetParentStatus == nil {
		route.Status.Parents = append(route.Status.Parents, gwtypes.RouteParentStatus{
			ParentRef:      pRef,
			ControllerName: gwtypes.GatewayController(controllerName),
			Conditions:     conditions,
		})
		// We are done, return.
		return true
	}

	// Process each condition.
	for _, newCondition := range conditions {
		// Find existing condition of the same type.
		existingConditionIndex := -1
		for i, existingCondition := range targetParentStatus.Conditions {
			if existingCondition.Type == newCondition.Type {
				existingConditionIndex = i
				break
			}
		}

		// Compare conditions and update if different.
		if existingConditionIndex >= 0 {
			existingCondition := &targetParentStatus.Conditions[existingConditionIndex]
			if !isConditionEqual(*existingCondition, newCondition) {
				// Update existing condition.
				existingCondition.Status = newCondition.Status
				existingCondition.Reason = newCondition.Reason
				existingCondition.Message = newCondition.Message
				existingCondition.ObservedGeneration = newCondition.ObservedGeneration
				existingCondition.LastTransitionTime = metav1.Now()
				updated = true
			}
		} else {
			// Add new condition.
			newCondition.LastTransitionTime = metav1.Now()
			targetParentStatus.Conditions = append(targetParentStatus.Conditions, newCondition)
			updated = true
		}
	}

	return updated
}

// CleanupOrphanedParentStatus removes ParentStatus entries from the route status that are no longer
// relevant (i.e., not present in the route's current parentRefs) and are managed by the specified controller.
// This function helps maintain a clean status by removing stale entries when parentRefs are removed from the route spec.
//
// Parameters:
//   - route: The HTTPRoute to clean up
//   - controllerName: The controller name that should own the ParentStatus entries to be cleaned
//
// Returns:
//   - bool: true if any ParentStatus entries were removed, false if no changes were made
//
// This function only removes ParentStatus entries owned by the specified controller, leaving
// entries from other controllers untouched, making it safe for use in multi-controller environments.
func CleanupOrphanedParentStatus(logger logr.Logger, route *gwtypes.HTTPRoute, controllerName string) bool {
	if len(route.Status.Parents) == 0 {
		return false
	}

	// Create a set of current parentRefs for quick lookup.
	currentParentRefs := make(map[string]bool)
	for _, pRef := range route.Spec.ParentRefs {
		// Create a unique key for each parentRef.
		key := parentRefKey(pRef)
		currentParentRefs[key] = true
	}

	// Filter out orphaned ParentStatus entries.
	var filteredParents []gwtypes.RouteParentStatus
	removed := false

	for _, parentStatus := range route.Status.Parents {
		// Only process entries owned by our controller.
		if string(parentStatus.ControllerName) != controllerName {
			// Keep entries from other controllers.
			filteredParents = append(filteredParents, parentStatus)
			continue
		}

		// Check if this ParentStatus corresponds to a current parentRef.
		key := parentRefKey(parentStatus.ParentRef)
		if currentParentRefs[key] {
			// Keep relevant entries.
			filteredParents = append(filteredParents, parentStatus)
		} else {
			// Mark as removed (orphaned entry).
			logger.V(1).Info("Removing orphaned ParentStatus entry", "parentRef", parentStatus.ParentRef, "controller", controllerName)
			removed = true
		}
	}

	// Update the route status if we removed any entries
	if removed {
		route.Status.Parents = filteredParents
	}

	return removed
}

// RemoveStatusForParentRef removes the ParentStatus entry for the specified ParentReference and controllerName from the route's status.
//
// This function iterates through all ParentStatus entries in the route's status and removes any entry that matches both the given
// ParentReference and controllerName. It is useful for cleaning up status when a parent reference is deleted or no longer relevant.
//
// Parameters:
//   - route: The HTTPRoute whose status should be updated
//   - pRef: The ParentReference to match for removal
//   - controllerName: The controller name that must match for removal
//
// Returns:
//   - bool: true if a matching ParentStatus entry was removed, false otherwise
func RemoveStatusForParentRef(logger logr.Logger, route *gwtypes.HTTPRoute, pRef gwtypes.ParentReference, controllerName string) bool {
	if len(route.Status.Parents) == 0 {
		return false
	}
	removed := false
	var filteredParents []gwtypes.RouteParentStatus
	for _, parentStatus := range route.Status.Parents {
		if string(parentStatus.ControllerName) == controllerName && isParentRefEqual(parentStatus.ParentRef, pRef) {
			// Skip this entry (remove it).
			logger.V(1).Info("Removing ParentStatus entry for ParentReference", "parentRef", parentStatus.ParentRef, "controller", controllerName)
			removed = true
			continue
		}
		filteredParents = append(filteredParents, parentStatus)
	}
	if removed {
		route.Status.Parents = filteredParents
	}
	return removed
}

// parentRefKey generates a unique key for a ParentReference to enable comparison
func parentRefKey(pRef gwtypes.ParentReference) string {
	var group, kind, namespace, sectionName string
	var port string

	if pRef.Group != nil {
		group = string(*pRef.Group)
	}
	if pRef.Kind != nil {
		kind = string(*pRef.Kind)
	}
	if pRef.Namespace != nil {
		namespace = string(*pRef.Namespace)
	}
	if pRef.SectionName != nil {
		sectionName = string(*pRef.SectionName)
	}
	if pRef.Port != nil {
		port = fmt.Sprintf("%d", *pRef.Port)
	}

	return fmt.Sprintf("%s/%s/%s/%s/%s/%s", group, kind, namespace, string(pRef.Name), sectionName, port)
}

// isParentRefEqual compares two ParentReference objects for equality
func isParentRefEqual(a, b gwtypes.ParentReference) bool {
	// Compare Group
	if (a.Group == nil) != (b.Group == nil) {
		return false
	}

	if a.Group != nil && b.Group != nil && *a.Group != *b.Group {
		return false
	}

	// Compare Kind
	if (a.Kind == nil) != (b.Kind == nil) {
		return false
	}
	if a.Kind != nil && b.Kind != nil && *a.Kind != *b.Kind {
		return false
	}

	// Compare Name
	if a.Name != b.Name {
		return false
	}

	// Compare Namespace
	if (a.Namespace == nil) != (b.Namespace == nil) {
		return false
	}
	if a.Namespace != nil && b.Namespace != nil && *a.Namespace != *b.Namespace {
		return false
	}

	// Compare SectionName
	if (a.SectionName == nil) != (b.SectionName == nil) {
		return false
	}
	if a.SectionName != nil && b.SectionName != nil && *a.SectionName != *b.SectionName {
		return false
	}

	// Compare Port
	if (a.Port == nil) != (b.Port == nil) {
		return false
	}
	if a.Port != nil && b.Port != nil && *a.Port != *b.Port {
		return false
	}

	return true
}

// isConditionEqual compares two conditions to see if they are functionally equivalent
// (ignoring LastTransitionTime since that's updated automatically)
func isConditionEqual(a, b metav1.Condition) bool {
	return a.Type == b.Type &&
		a.Status == b.Status &&
		a.Reason == b.Reason &&
		a.Message == b.Message &&
		a.ObservedGeneration == b.ObservedGeneration
}

// Predefined errors returned by GetSupportedGatewayForParentRef to indicate
// specific reasons why a ParentReference is not supported by this controller.
var (
	// ErrNoGatewayFound is returned when the Gateway referenced by a ParentReference does not exist in the cluster.
	ErrNoGatewayFound = fmt.Errorf("no supported gateway found")

	// ErrNoGatewayClassFound is returned when the GatewayClass referenced by a Gateway does not exist in the cluster.
	ErrNoGatewayClassFound = fmt.Errorf("no gatewayClass found for gateway")

	// ErrNoGatewayController is returned when the GatewayClass exists but is not controlled by this controller.
	ErrNoGatewayController = fmt.Errorf("gatewayClass is not controlled by this controller")
)

// GetSupportedGatewayForParentRef checks if the given ParentReference is supported by this controller
// and returns the associated Gateway if it is supported.
//
// The function validates that:
// - The ParentRef refers to a Gateway kind (not other resource types).
// - The ParentRef uses the gateway.networking.k8s.io group (or defaults to it).
// - The referenced Gateway exists in the cluster.
// - The Gateway's GatewayClass exists and is controlled by this controller.
//
// Parameters:
//   - ctx: The context for API calls.
//   - logger: Logger for debugging information.
//   - cl: The Kubernetes client for API operations.
//   - pRef: The ParentReference to validate.
//   - routeNamespace: The namespace of the route (used as default if ParentRef namespace is unspecified).
//
// Returns:
//   - *gwtypes.Gateway: The Gateway object if the ParentRef is supported, nil if not supported.
//   - error: Specific error indicating why the ParentRef is not supported, or nil if validation passes.
//
// The function returns specific errors to help callers understand why a ParentRef is not supported:
// - ErrNoGatewayFound: The referenced Gateway doesn't exist.
// - ErrNoGatewayClassFound: The Gateway's GatewayClass doesn't exist.
// - ErrNoGatewayController: The GatewayClass is not controlled by this controller.
// - nil error with nil Gateway: The ParentRef is valid but not supported (wrong kind/group).
func GetSupportedGatewayForParentRef(ctx context.Context, logger logr.Logger, cl client.Client, pRef gwtypes.ParentReference,
	routeNamespace string) (*gwtypes.Gateway, error) {
	// Only support Gateway kind.
	if pRef.Kind != nil && *pRef.Kind != "Gateway" {
		logger.V(1).Info("Ignoring ParentReference, unsupported kind", "pRef", pRef, "kind", *pRef.Kind)
		return nil, nil
	}

	// Only support gateway.networking.k8s.io group (or empty group which defaults to this).
	if pRef.Group != nil && *pRef.Group != "gateway.networking.k8s.io" {
		logger.V(1).Info("Ignoring ParentReference, unsupported group", "pRef", pRef, "group", *pRef.Group)
		return nil, nil
	}

	// Determine the namespace - use route's namespace if not specified.
	namespace := routeNamespace
	if pRef.Namespace != nil {
		namespace = string(*pRef.Namespace)
	}

	// Get the Gateway object.
	gateway := gwtypes.Gateway{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: string(pRef.Name)}, &gateway); err != nil {
		if k8serrors.IsNotFound(err) {
			// Gateway doesn't exist, not supported.
			return nil, ErrNoGatewayFound
		}
		return nil, fmt.Errorf("failed to get gateway for ParentReference %v: %w", pRef, err)
	}

	// Check if the gatewayClass exists.
	gatewayClass := gwtypes.GatewayClass{}
	if err := cl.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &gatewayClass); err != nil {
		if k8serrors.IsNotFound(err) {
			// GatewayClass doesn't exist, not supported.
			return nil, ErrNoGatewayClassFound
		}
		return nil, fmt.Errorf("failed to get gatewayClass for parentReference %v: %w", pRef, err)
	}

	// Check if the gatewayClass is controlled by us.
	// If not, we just ignore it and return nil.
	if string(gatewayClass.Spec.ControllerName) != vars.ControllerName() {
		return nil, ErrNoGatewayController
	}

	// All checks passed - this ParentRef is supported
	return &gateway, nil
}

// BuildAcceptedCondition builds the "Accepted" condition for a given HTTPRoute and ParentReference.
// It validates whether the route can be accepted by the gateway by checking multiple criteria
// in sequence, returning the first failure condition encountered or a success condition if all checks pass.
//
// The function performs the following validation steps:
// 1. Filters listeners that match the ParentReference (section name, port, protocol)
// 2. Checks if the route namespace is allowed by the gateway listeners
// 3. Validates that route hostnames intersect with listener hostnames
//
// Parameters:
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//   - cl: The Kubernetes client for API operations
//   - gateway: The Gateway object associated with the ParentReference
//   - route: The HTTPRoute to validate for acceptance
//   - pRef: The ParentReference being evaluated
//
// Returns:
//   - *metav1.Condition: The "Accepted" condition with appropriate status, reason, and message
//   - error: Any error that occurred during validation (e.g., failed to retrieve namespace)
//
// The function returns a condition with status "False" if any validation step fails,
// or status "True" if the route is accepted by the gateway. The condition includes
// specific reasons and messages to help diagnose acceptance issues.
func BuildAcceptedCondition(ctx context.Context, logger logr.Logger, cl client.Client, gateway *gwtypes.Gateway,
	route *gwtypes.HTTPRoute, pRef gwtypes.ParentReference) (*metav1.Condition, error) {
	// Begin by excluding listeners that do not match the specified section name.
	listeners, cond := FilterMatchingListeners(logger, gateway, pRef, gateway.Spec.Listeners)
	if cond != nil {
		logger.V(1).Info("No matching listeners for ParentReference", "parentRef", pRef, "gateway", gateway.Name)
		// Return the condition indicating no matching listeners.
		return SetConditionMeta(*cond, route), nil
	}

	// If we have listeners that match, we check the allowed routes.
	// We need to get the namespace of the route to check against the allowed routes.
	routeNamespace := corev1.Namespace{}
	if err := cl.Get(ctx, client.ObjectKey{Name: route.Namespace}, &routeNamespace); err != nil {
		return nil, fmt.Errorf("failed to get namespace %s for route %s while building accepted condition for gateway %s: %w",
			route.Namespace, client.ObjectKeyFromObject(route), client.ObjectKeyFromObject(gateway), err)
	}
	// Prepare the RouteGroupKind for the HTTPRoute.
	rgk := GetRouteGroupKind(route)
	// Filter listeners by allowed routes.
	listeners, cond, err := FilterListenersByAllowedRoutes(logger, gateway, pRef, listeners, rgk, &routeNamespace)
	if err != nil {
		return nil, err
	}
	if cond != nil {
		logger.V(1).Info("Listeners do not allow route", "parentRef", pRef, "gateway", gateway.Name, "reason", cond.Reason)
		return SetConditionMeta(*cond, route), nil
	}

	// If we have listeners that allow the route, we check the hostnames.
	_, cond = FilterListenersByHostnames(logger, listeners, route.Spec.Hostnames)
	if cond != nil {
		logger.V(1).Info("Listeners do not match hostnames", "parentRef", pRef, "gateway", gateway.Name, "reason", cond.Reason)
		return SetConditionMeta(*cond, route), nil
	}

	// If we have listeners that match the hostnames, we can accept the route.
	logger.V(1).Info("Route accepted by gateway", "route", route.Name, "gateway", gateway.Name)
	cond = &metav1.Condition{
		Type:    string(gwtypes.RouteConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gwtypes.RouteReasonAccepted),
		Message: "The route is accepted by the gateway",
	}
	return SetConditionMeta(*cond, route), nil
}

// BuildProgrammedCondition evaluates the programmed status of all resources associated with a route and gateway.
// For each expected GroupVersionKind (GVK), it lists resources owned by the route and gateway, checks if each is programmed,
// and generates a corresponding condition (True if programmed, False otherwise).
//
// The function deduplicates conditions by type, keeping the most severe status for each resource type.
//
// Parameters:
//   - ctx: Context for API calls.
//   - logger: Logger for debugging information.
//   - cl: Kubernetes client for resource operations.
//   - route: The HTTPRoute whose resources are being checked.
//   - pRef: The ParentReference (gateway) associated with the route.
//   - expectedGVKs: Slice of GVKs representing the resource types to check.
//
// Returns:
//   - []metav1.Condition: Slice of conditions, one per resource type, indicating programmed status.
//   - error: Any error encountered during resource listing or evaluation.
//
// This function provides granular feedback for each resource type, allowing users to see exactly which
// resources are not programmed and why, improving troubleshooting and status visibility.
func BuildProgrammedCondition(ctx context.Context, logger logr.Logger, cl client.Client, route *gwtypes.HTTPRoute,
	pRef gwtypes.ParentReference, expectedGVKs []schema.GroupVersionKind) ([]metav1.Condition, error) {
	var conditions []metav1.Condition
	ns := route.GetNamespace()

	// For each expected GVK, list resources owned by the route and gateway.
	for _, gvk := range expectedGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		selector := metadata.LabelSelectorForOwnedResources(route, &pRef)

		// List all resources of this GVK owned by the route object in the same namespace.
		if err := cl.List(ctx, list, selector, client.InNamespace(ns)); err != nil {
			return nil, fmt.Errorf("unable to list objects with gvk %s in namespace %s: %w", gvk.String(), ns, err)
		}

		for _, item := range list.Items {
			// Check if the item is programmed.
			prog := isProgrammed(&item)
			logger.V(1).Info("Resource programmed status", "gvk", gvk.String(), "name", item.GetName(), "namespace", item.GetNamespace(), "programmed", prog)
			conditions = append(conditions, *SetConditionMeta(GetProgrammedConditionForGVK(gvk, prog), route))
		}
	}

	// Before returning, deduplicate conditions by type, keeping the most severe status.
	return DeduplicateConditionsByType(conditions), nil
}

// isProgrammed checks if an unstructured Kubernetes object has a "Programmed" condition with status "True".
// This function is used to determine if resources (like Kong CRDs) are ready and operational.
//
// The function safely navigates the object's status.conditions array to find a condition with:
// - type: "Programmed"
// - status: "True"
//
// Parameters:
//   - obj: An unstructured Kubernetes object that may contain status conditions
//
// Returns:
//   - bool: true if the object has a "Programmed" condition with status "True", false otherwise
//
// The function returns false in the following cases:
// - The object has no status.conditions field
// - An error occurs while accessing the conditions
// - No "Programmed" condition is found
// - The "Programmed" condition exists but status is not "True"
//
// This function is commonly used to check if Kong resources (Services, Routes, Upstreams, etc.)
// have been successfully programmed into the Kong data plane.
func isProgrammed(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, conditionInterface := range conditions {
		condition, ok := conditionInterface.(map[string]any)
		if !ok {
			continue
		}

		conditionType, found, err := unstructured.NestedString(condition, "type")
		if err != nil || !found {
			continue
		}

		if conditionType == "Programmed" {
			status, found, err := unstructured.NestedString(condition, "status")
			if err != nil || !found {
				continue
			}
			return status == "True"
		}
	}
	return false
}

// FilterMatchingListeners filters the provided listeners to find those that match the given ParentReference
// based on section name, port, and protocol requirements. Only listeners that are both matching and ready
// are returned.
//
// The function performs the following checks for each listener:
// 1. Matches the section name specified in the ParentReference (if any)
// 2. Matches the port specified in the ParentReference (if any)
// 3. Supports HTTP/HTTPS protocol with appropriate TLS termination mode
// 4. Is in a "Programmed" (ready) state according to the Gateway status
//
// Parameters:
//   - gw: The Gateway object containing the listeners and their status
//   - logger: Logger for debugging information
//   - pRef: The ParentReference specifying matching criteria
//   - listeners: The list of listeners from the Gateway spec to filter
//
// Returns:
//   - []gwtypes.Listener: List of listeners that match criteria and are ready
//   - *metav1.Condition: Condition indicating why no listeners matched (nil if matches found)
//
// The returned condition will have status "False" with reason "NoMatchingParent" if no listeners
// match the criteria. If listeners match but are not ready, the message will indicate this distinction.
func FilterMatchingListeners(logger logr.Logger, gw *gwtypes.Gateway, pRef gwtypes.ParentReference, listeners []gwtypes.Listener) ([]gwtypes.Listener, *metav1.Condition) {
	var matchingListeners []gwtypes.Listener
	var matchedNotReady bool
	for _, listener := range listeners {
		// Check if the listener name matches the section name of the parent reference.
		if pRef.SectionName != nil && *pRef.SectionName != listener.Name {
			continue
		}

		// Check if the parent reference port matches the listener port, if specified.
		if pRef.Port != nil && *pRef.Port != listener.Port {
			continue
		}

		// Check if the protocol matches.
		// HTTPRoutes support Terminate only
		// Note: this is a guess we are doing as the upstream documentation is unclear at the moment.
		// see https://github.com/kubernetes-sigs/gateway-api/issues/1474
		if listener.Protocol != gwtypes.HTTPProtocolType && listener.Protocol != gwtypes.HTTPSProtocolType {
			continue
		}
		if listener.TLS != nil && *listener.TLS.Mode != gwtypes.TLSModeTerminate {
			continue
		}

		// At this point, the listener matches the parent reference.
		logger.V(1).Info("Listener matches ParentReference criteria", "listener", listener.Name, "parentRef", pRef)
		// Now check if the listener is ready.
		for _, ls := range gw.Status.Listeners {
			if ls.Name == listener.Name {
				for _, cond := range ls.Conditions {
					if cond.Type == string(gwtypes.ListenerConditionProgrammed) {
						if cond.Status == metav1.ConditionTrue {
							logger.V(1).Info("Listener is ready (programmed)", "listener", listener.Name)
							matchingListeners = append(matchingListeners, listener)
						} else {
							logger.V(1).Info("Listener matched but is not ready", "listener", listener.Name)
							// Listener is not ready, and we track that at least one listener matched but is not ready.
							matchedNotReady = true
						}
					}
				}
			}
		}
	}

	// If we found no matching listeners, determine the appropriate condition to return.
	if len(matchingListeners) == 0 {
		// If we had at least one listener that matched but was not ready, return a condition indicating that.
		msg := string(gwtypes.RouteReasonNoMatchingParent)
		if matchedNotReady {
			msg = "A Gateway Listener matches this route but is not ready"
		}
		logger.V(1).Info("No matching listeners found for ParentReference", "parentRef", pRef, "matchedNotReady", matchedNotReady)
		return nil, &metav1.Condition{
			Type:    string(gwtypes.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gwtypes.RouteReasonNoMatchingParent),
			Message: msg,
		}
	}
	// Return the list of matching and ready listeners that can be used for further processing.
	logger.V(1).Info("Matching and ready listeners found", "parentRef", pRef, "count", len(matchingListeners))
	return matchingListeners, nil
}

// FilterListenersByAllowedRoutes filters the provided listeners to find those that allow the given HTTPRoute based on its kind and namespace.
// The function checks each listener's AllowedRoutes configuration to determine if the route is permitted.
//
// The function performs the following validation steps:
// 1. Checks if the route kind is allowed by the listener's AllowedRoutes.Kinds
// 2. Checks if the route namespace is allowed by the listener's AllowedRoutes.Namespaces configuration
// 3. Handles different namespace selection modes (All, Same, Selector)
//
// Parameters:
//   - gw: The Gateway object containing the listeners
//   - logger: Logger for debugging information
//   - pRef: The ParentReference being evaluated
//   - listeners: The list of listeners to filter
//   - rgk: The RouteGroupKind of the route being validated
//   - routeNamespace: The namespace object of the route
//
// Returns:
//   - []gwtypes.Listener: List of listeners that allow this route
//   - *metav1.Condition: Condition indicating why no listeners allow the route (nil if matches found)
//   - error: Any error that occurred during validation (e.g., invalid label selector)
func FilterListenersByAllowedRoutes(logger logr.Logger, gw *gwtypes.Gateway, pRef gwtypes.ParentReference, listeners []gwtypes.Listener, rgk gwtypes.RouteGroupKind, routeNamespace *corev1.Namespace) ([]gwtypes.Listener, *metav1.Condition, error) {
	var matchingListeners []gwtypes.Listener

	for _, listener := range listeners {
		if listener.AllowedRoutes == nil {
			// If AllowedRoutes is nil, all routes are allowed.
			logger.V(1).Info("Listener allows all routes (AllowedRoutes is nil)", "listener", listener.Name)
			matchingListeners = append(matchingListeners, listener)
			continue
		}

		// Check if the route kind is allowed.
		if len(listener.AllowedRoutes.Kinds) > 0 {
			matched := false
			for _, allowedKind := range listener.AllowedRoutes.Kinds {
				if (allowedKind.Group != nil && *allowedKind.Group != *rgk.Group) || allowedKind.Kind != rgk.Kind {
					continue
				} else {
					logger.V(1).Info("Listener allows route kind", "listener", listener.Name, "allowedKind", allowedKind, "routeKind", rgk)
					matched = true
					break
				}
			}
			if !matched {
				logger.V(1).Info("Listener does not allow route kind", "listener", listener.Name, "routeKind", rgk)
				// If no allowed kind matched, we can't use this listener.
				continue
			}
		}

		// Check if the route namespace is allowed.
		if listener.AllowedRoutes.Namespaces == nil || listener.AllowedRoutes.Namespaces.From == nil {
			// If Namespaces or From is nil, all namespaces are allowed.
			logger.V(1).Info("Listener allows all namespaces (Namespaces or From is nil)", "listener", listener.Name)
			matchingListeners = append(matchingListeners, listener)
			continue
		}

		switch *listener.AllowedRoutes.Namespaces.From {
		case gwtypes.NamespacesFromAll:
			// All namespaces are allowed.
			logger.V(1).Info("Listener allows all namespaces (NamespacesFromAll)", "listener", listener.Name)
			matchingListeners = append(matchingListeners, listener)
		case gwtypes.NamespacesFromSame:
			// Only the same namespace as the gateway is allowed.
			if (pRef.Namespace != nil && string(*pRef.Namespace) == routeNamespace.Name) || (pRef.Namespace == nil && gw.Namespace == routeNamespace.Name) {
				logger.V(1).Info("Listener allows same namespace as gateway", "listener", listener.Name, "routeNamespace", routeNamespace.Name)
				matchingListeners = append(matchingListeners, listener)
			}
		case gwtypes.NamespacesFromSelector:
			// Only namespaces matching the selector are allowed.
			if listener.AllowedRoutes.Namespaces.Selector == nil {
				logger.V(1).Info("Listener has NamespacesFromSelector but selector is nil", "listener", listener.Name)
				// If Selector is nil, no namespaces are allowed.
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
			if err != nil {
				logger.Error(err, "Failed to convert AllowedRoutes.Namespaces.Selector", "listener", listener.Name, "selector", listener.AllowedRoutes.Namespaces.Selector)
				return nil, nil, fmt.Errorf("failed to convert AllowedRoutes.Namespaces.Selector %s to selector for listener %s for gateway %s: %w",
					listener.AllowedRoutes.Namespaces.Selector, listener.Name, client.ObjectKeyFromObject(gw), err)
			}

			if selector.Matches(labels.Set(routeNamespace.Labels)) {
				logger.V(1).Info("Listener allows route namespace by selector", "listener", listener.Name, "routeNamespace", routeNamespace.Name)
				matchingListeners = append(matchingListeners, listener)
			}
		default:
			// Unknown value for From, skip this listener.
			return nil, nil, fmt.Errorf("unknown value for AllowedRoutes.Namespaces.From: %s for listener %s for gateway %s",
				*listener.AllowedRoutes.Namespaces.From, listener.Name, client.ObjectKeyFromObject(gw))
		}
	}

	// If we found no matching listeners, return a condition indicating the reason.
	if len(matchingListeners) == 0 {
		logger.V(1).Info("No listeners allow this route", "parentRef", pRef, "routeKind", rgk, "routeNamespace", routeNamespace.Name)
		return nil, &metav1.Condition{
			Type:    string(gwtypes.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gwtypes.RouteReasonNotAllowedByListeners),
			Message: "No Gateway Listener allows this route",
		}, nil
	}

	// Return the list of matching listeners that can be used for further processing.
	logger.V(1).Info("Listeners found that allow this route", "parentRef", pRef, "count", len(matchingListeners))
	return matchingListeners, nil, nil
}

// FilterListenersByHostnames filters the provided listeners to find those that match any of the given hostnames.
// The function checks if there is an intersection between the listener's hostname and the route's hostnames.
// If a listener has no hostname specified, it matches all hostnames (wildcard behavior).
//
// The function performs hostname intersection validation:
// 1. If listener has no hostname, it accepts all route hostnames
// 2. For each route hostname, checks if it intersects with the listener hostname
// 3. Uses hostname intersection logic to handle wildcards and exact matches
//
// Parameters:
//   - listeners: The list of listeners to filter based on hostname matching
//   - logger: Logger for debugging information
//   - hostnames: The list of hostnames from the HTTPRoute spec to match against
//
// Returns:
//   - []gwtypes.Listener: List of listeners that have hostname intersection with the route
//   - *metav1.Condition: Condition indicating why no listeners matched hostnames (nil if matches found)
//
// The returned condition will have status "False" with reason "NoMatchingListenerHostname" if no listeners
// have hostname intersection with the route. If matching listeners are found, the condition will be nil.
func FilterListenersByHostnames(logger logr.Logger, listeners []gwtypes.Listener, hostnames []gwtypes.Hostname) ([]gwtypes.Listener, *metav1.Condition) {
	var matchingListeners []gwtypes.Listener
	for _, listener := range listeners {
		// If the listener has no hostname, it matches all hostnames.
		if listener.Hostname == nil || *listener.Hostname == "" {
			logger.V(1).Info("Listener matches all hostnames (wildcard)", "listener", listener.Name)
			matchingListeners = append(matchingListeners, listener)
			continue
		}

		// Check if any of the route hostnames match the listener hostname.
		for _, hostname := range hostnames {
			routeHostname := string(hostname)
			if intersection := utils.HostnameIntersection(string(*listener.Hostname), routeHostname); intersection != "" {
				logger.V(1).Info("Listener matches route hostname", "listener", listener.Name, "listenerHostname", *listener.Hostname, "routeHostname", routeHostname)
				matchingListeners = append(matchingListeners, listener)
				break
			}
			logger.V(1).Info("Listener does not match route hostname", "listener", listener.Name, "listenerHostname", *listener.Hostname, "routeHostname", routeHostname)
		}
	}
	if len(matchingListeners) == 0 {
		logger.V(1).Info("No listeners match route hostnames", "hostnames", hostnames)
		return nil, &metav1.Condition{
			Type:    string(gwtypes.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gwtypes.RouteReasonNoMatchingListenerHostname),
			Message: "No Gateway Listener hostname matches this route",
		}
	}

	logger.V(1).Info("Listeners found that match route hostnames", "count", len(matchingListeners), "hostnames", hostnames)
	return matchingListeners, nil
}

// GetRouteGroupKind returns the RouteGroupKind for a given route object, ensuring the group is set to the Gateway API default if empty.
// This function extracts the GroupVersionKind from the route object and converts it to the Gateway API RouteGroupKind format.
// If the group is empty, it defaults to "gateway.networking.k8s.io" which is the standard Gateway API group.
//
// Parameters:
//   - route: The route object (HTTPRoute, GRPCRoute, etc.) to extract GroupKind information from
//
// Returns:
//   - gwtypes.RouteGroupKind: The RouteGroupKind with proper group and kind set for Gateway API validation
//
// This function is typically used when validating routes against Gateway listeners' AllowedRoutes configuration.
func GetRouteGroupKind(route client.Object) gwtypes.RouteGroupKind {
	gvk := route.GetObjectKind().GroupVersionKind()
	group := gvk.Group
	if group == "" {
		group = "gateway.networking.k8s.io"
	}
	grp := gwtypes.Group(group)
	return gwtypes.RouteGroupKind{
		Group: &grp,
		Kind:  gwtypes.Kind(gvk.Kind),
	}
}

// SetConditionMeta sets the ObservedGeneration and LastTransitionTime fields on a condition using the given route.
// This function populates metadata fields that are required for proper condition tracking in Kubernetes resources.
//
// The function sets:
// - ObservedGeneration: Set to the route's current generation to indicate which version was observed
// - LastTransitionTime: Set to the current timestamp to track when the condition was last updated
//
// Parameters:
//   - cond: The condition to update with metadata
//   - route: The HTTPRoute object to extract generation information from
//
// Returns:
//   - *metav1.Condition: Pointer to the updated condition with metadata fields set
//
// This function is typically called before setting conditions in route status to ensure proper
// condition metadata tracking according to Kubernetes conventions.
func SetConditionMeta(cond metav1.Condition, route *gwtypes.HTTPRoute) *metav1.Condition {
	cond.ObservedGeneration = route.Generation
	cond.LastTransitionTime = metav1.Now()
	return &cond
}
