package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func getHybridGatewayParents[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](
	ctx context.Context, logger logr.Logger,
	cl client.Client, route TPtr,
) ([]hybridGatewayParent, error) {
	parentRefs := gwtypes.GetSpecParentRefs(*route)
	log.Debug(logger, "Getting hybrid gateway parents", "parentRefCount", len(parentRefs))

	result := []hybridGatewayParent{}
	for i, pRef := range parentRefs {
		log.Debug(logger, "Processing parent reference", "index", i, "parentRef", pRef)

		cp, err := refs.GetControlPlaneRefByParentRef(ctx, logger, cl, route, pRef)
		if err != nil {
			switch {
			case errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup):
				// These are expected errors to be handled gracefully. Log and skip this ParentRef, continue with others.
				log.Debug(logger, "Skipping ParentRef due to expected error", "parentRef", pRef, "error", err)
				continue
			default:
				// Unexpected system error, fail the entire translation.
				return nil, fmt.Errorf("failed to get ControlPlaneRef for ParentRef %s: %w", pRef.Name, err)
			}
		}

		if cp == nil {
			log.Debug(logger, "No ControlPlaneRef found for ParentRef, skipping", "parentRef", pRef)
			continue
		}

		log.Debug(logger, "Found ControlPlaneRef for ParentRef", "parentRef", pRef, "controlPlane", cp.KonnectNamespacedRef)

		hostnames, err := getHostnamesByParentRef(ctx, logger, cl, route, pRef)
		if err != nil {
			log.Error(logger, err, "Failed to get hostnames for ParentRef", "parentRef", pRef)
			return nil, err
		}
		if hostnames == nil {
			log.Debug(logger, "No hostnames found for ParentRef, skipping", "parentRef", pRef)
			continue
		}

		log.Debug(logger, "Adding parent reference to result", "parentRef", pRef, "hostnames", hostnames)
		result = append(result, hybridGatewayParent{
			parentRef: pRef,
			cpRef:     cp,
			hostnames: hostnames,
		})
	}

	log.Debug(logger, "Finished processing parent references", "supportedParents", len(result))
	return result, nil
}

// getHostnamesByParentRef returns the hostnames that match between the route and the Gateway listeners.
func getHostnamesByParentRef[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](
	ctx context.Context, logger logr.Logger, cl client.Client, route TPtr, pRef gwtypes.ParentReference,
) ([]string, error) {
	logger = logger.WithValues("parentRef", pRef.Name)
	log.Debug(logger, "Getting hostnames for ParentRef")

	var err error
	var hostnames []string
	routeHostnames := routeHostNamesString(*route)

	listeners, err := refs.GetListenersByParentRef(ctx, cl, route, pRef)
	if err != nil {
		log.Error(logger, err, "Failed to get listeners for ParentRef")
		return nil, err
	}

	log.Debug(logger, "Found listeners for ParentRef", "listenerCount", len(listeners))

	for _, listener := range listeners {
		// Check section reference if present
		if pRef.SectionName != nil {
			sectionName := string(*pRef.SectionName)
			if string(listener.Name) != sectionName {
				// This listener doesn't match the section reference, skip it
				continue
			}
		}
		if pRef.Port != nil {
			if listener.Port != lo.FromPtr(pRef.Port) {
				// This listener doesn't match the port reference, skip it
				continue
			}
		}

		if isHostlessRoute(route) {
			log.Debug(logger, "Route does not use hostname matching", "listener", listener.Name)
			return []string{}, nil
		}

		// If the listener has no hostname, it means it accepts all HTTPRoute hostnames.
		// No need to do further checks.
		if listener.Hostname == nil || *listener.Hostname == "" {
			log.Debug(logger, "Listener accepts all hostnames", "listener", listener.Name)
			return routeHostnames, nil
		}

		// If the route does not specify hostnames, it matches all listener hostnames.
		if len(routeHostnames) == 0 {
			hostnames = append(hostnames, string(*listener.Hostname))
			continue
		}

		// Handle wildcard hostnames - get intersection
		log.Debug(logger, "Processing listener with hostname", "listener", listener.Name, "listenerHostname", *listener.Hostname)
		for _, host := range routeHostnames {
			if intersection, ok := utils.HostnameIntersection(string(*listener.Hostname), host); ok {
				log.Trace(logger, "Found hostname intersection", "listenerHostname", *listener.Hostname, "routeHostname", host, "intersection", intersection)
				hostnames = append(hostnames, intersection)
			}
		}
	}

	hostnames = lo.Uniq(hostnames)
	if len(hostnames) == 0 {
		// Returning nil tells the caller to skip this parent entirely. An empty slice
		// would flow into WithHosts() and create a host-less KongRoute that matches any host.
		log.Debug(logger, "No hostname intersection found for ParentRef")
		return nil, nil
	}

	log.Debug(logger, "Finished processing hostnames", "finalHostnames", hostnames)
	return hostnames, nil
}

func isHostlessRoute[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](route TPtr) bool {
	switch any(route).(type) {
	case *gwtypes.TCPRoute:
		return true
	case *gwtypes.UDPRoute:
		return true
	default:
		return false
	}
}

func routeHostNamesString[T gwtypes.SupportedRoute](route T) []string {
	return lo.Map(gwtypes.GetSpecHostnames(route), func(h gwtypes.Hostname, _ int) string {
		return string(h)
	})
}

// deduplicateOutputStore collapses objects that share the same type, namespace and name, keeping
// the first occurrence.
//
// The translate() loop iterates over (parentRef × rule) and appends the generated Kong resources on
// every iteration. The KongUpstream/KongService/KongTarget names are derived from
// (route, controlPlaneRef, rule.BackendRefs) and deliberately exclude the parentRef and the rule
// index (see namegen.hashElementsForServiceLikeName / hashForHTTPRouteRuleServiceLikeName, hashed
// over rule.BackendRefs). As a result the exact same service-like object is produced more than once
// when:
//
//  1. Multiple rules of the same Route reference the same backend: the name hash is over
//     rule.BackendRefs, so rules with identical backends collapse onto the same
//     KongUpstream/KongService/KongTargets (only the per-match KongRoutes differ).
//  2. A Route has multiple parentRefs that resolve to the same ControlPlane: since the names omit
//     the parentRef, each parent re-generates the same service-like resources for every rule.
//
// These shared resources are intentional (one shared backend -> one shared KongService/KongUpstream),
// but because we append once per (parentRef, rule) the output store must be deduplicated before it is
// applied and before it is compared against the live set during orphan cleanup. Note this is a
// distinct axis from target merging within a single rule (one KongTarget per unique endpoint across a
// rule's backendRefs), which is handled in target.TargetsForBackendRefs.
func deduplicateOutputStore(objects []client.Object) []client.Object {
	if len(objects) < 2 {
		return objects
	}

	seen := make(map[string]struct{}, len(objects))
	deduplicated := make([]client.Object, 0, len(objects))
	for _, obj := range objects {
		key := fmt.Sprintf("%T/%s/%s", obj, obj.GetNamespace(), obj.GetName())
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduplicated = append(deduplicated, obj)
	}

	return deduplicated
}

// convertOutputStoreToUnstructured converts a converter's outputStore into unstructured.Unstructured
// objects, using scheme for the type conversion. Shared by every APIConverter.GetOutputStore
// implementation, which otherwise differ only in the receiver type.
func convertOutputStoreToUnstructured(logger logr.Logger, scheme *runtime.Scheme, outputStore []client.Object) ([]unstructured.Unstructured, error) {
	logger = logger.WithValues("phase", "output-store-conversion")
	log.Debug(logger, "Starting output store conversion")

	var conversionErrors []error
	objects := make([]unstructured.Unstructured, 0, len(outputStore))
	for _, obj := range outputStore {
		unstr, err := utils.ToUnstructured(obj, scheme)
		if err != nil {
			conversionErr := fmt.Errorf("failed to convert %T %s to unstructured: %w", obj, obj.GetName(), err)
			conversionErrors = append(conversionErrors, conversionErr)
			log.Error(logger, err, "Failed to convert object to unstructured", "objectName", obj.GetName())
			continue
		}
		objects = append(objects, unstr)
	}

	if len(conversionErrors) > 0 {
		log.Error(logger, nil, "Output store conversion completed with errors",
			"totalObjectsAttempted", len(outputStore),
			"successfulConversions", len(objects),
			"conversionErrors", len(conversionErrors))
		return objects, fmt.Errorf("output store conversion failed with %d errors: %w", len(conversionErrors), errors.Join(conversionErrors...))
	}

	log.Debug(logger, "Successfully converted all objects in output store", "totalObjectsConverted", len(objects))
	return objects, nil
}

// handleOrphanedResourceForRoute implements the OrphanedResourceHandler logic shared by every
// route converter: it removes route from the shared hybrid-routes annotation on resource before
// orphan deletion, atomically. Multiple Routes (or rules) can share the same Kong resource, so a
// concurrent Route adding itself must not be lost and the resource must not be deleted while still
// referenced. We re-read the live object, drop our entry, and either patch with an optimistic lock
// (when other Routes remain) or surface the validated resourceVersion so the caller can delete with
// an optimistic-lock precondition. routeKind is the route's Kind (e.g. "TCPRoute"), used to check
// whether any other routes of the same kind remain in the annotation.
func handleOrphanedResourceForRoute(
	ctx context.Context, logger logr.Logger, cl client.Client, route client.Object, routeKind string,
	resource *unstructured.Unstructured,
) (skipDelete bool, err error) {
	am := metadata.NewAnnotationManager(logger)
	key := client.ObjectKeyFromObject(resource)
	gvk := resource.GroupVersionKind()

	fresh := &unstructured.Unstructured{}
	fresh.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, key, fresh); err != nil {
		if apierrors.IsNotFound(err) {
			// Already gone; nothing to delete.
			return true, nil
		}
		return true, fmt.Errorf("failed to get resource: %w", err)
	}

	// If the route is not present in the hybrid-routes annotation of the Kong resource, don't touch it.
	if !am.ContainsRoute(fresh, route) {
		log.Trace(logger, "Route annotation not found, skipping resource", "kind", fresh.GetKind(), "obj", key)
		return true, nil
	}

	base := fresh.DeepCopy()
	am.RemoveRouteFromAnnotation(fresh, route)

	// If other Routes are still present in the annotation, we just need to update the resource.
	if len(am.GetRoutesWithKind(fresh, routeKind)) > 0 {
		log.Debug(logger, "Updating hybrid-routes annotation", "kind", fresh.GetKind(), "obj", key)
		if err := cl.Patch(ctx, fresh, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return true, fmt.Errorf("failed to update resource: %w", err)
		}
		// Reflect the persisted state back onto the caller's resource.
		resource.SetAnnotations(fresh.GetAnnotations())
		resource.SetResourceVersion(fresh.GetResourceVersion())
		return true, nil
	}

	// No other routes remain. Surface the validated resourceVersion (and the annotation
	// removal) on the caller's resource so the orphan deletion uses it as an optimistic-lock
	// precondition, and don't skip deletion.
	resource.SetAnnotations(fresh.GetAnnotations())
	resource.SetResourceVersion(fresh.GetResourceVersion())
	return false, nil
}
