package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/kongroute"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/plugin"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/pluginbinding"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/service"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/target"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/upstream"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

var (
	_ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}
	_ DesiredStateReadinessChecker    = &httpRouteConverter{}
)

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	route         *gwtypes.HTTPRoute
	outputStore   []client.Object
	expectedGVKs  []schema.GroupVersionKind
	fqdnMode      bool
	clusterDomain string
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func newHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client, fqdnMode bool, clusterDomain string) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:        cl,
		outputStore:   []client.Object{},
		route:         httpRoute,
		fqdnMode:      fqdnMode,
		clusterDomain: clusterDomain,
		// IMPORTANT: The order of this slice is significant during resource cleanup operations.
		// While resources deletion order should take into account dependencies their main goal is to ensure safe cleanup preventing
		// security issues (e.g., scenarios where routes remain active while security plugins are deleted first).
		expectedGVKs: []schema.GroupVersionKind{
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongRoute"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongTarget"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongPluginBinding"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongService"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongCertificate"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongReferenceGrant"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongUpstream"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1.GroupVersion.Version, Kind: "KongPlugin"},
		},
	}
}

// GetRootObject implements APIConverter.
//
// Returns the HTTPRoute resource that this converter is managing. This method provides
// access to the original HTTPRoute object that was passed to the converter during creation.
//
// Returns:
//   - gwtypes.HTTPRoute: A copy of the HTTPRoute resource being converted
//
// This method is typically used by the reconciler to access metadata, labels, and other
// properties of the original HTTPRoute resource for status updates and resource management.
func (c *httpRouteConverter) GetRootObject() gwtypes.HTTPRoute {
	return *c.route
}

// Translate implements APIConverter.
//
// Performs the complete translation of an HTTPRoute resource into Kong-specific resources.
// This is the main entry point for the conversion process, delegating to the internal
// translate() method for the actual implementation.
//
// See the translate() method documentation for detailed information about the translation
// process, error handling strategy, and the specific Kong resources that are created.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging with httproute-translate phase
//
// Returns:
//   - int: Number of Kong resources created during translation
//   - error: Aggregated translation errors or nil if successful
func (c *httpRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	if err := c.translate(ctx, logger); err != nil {
		return 0, err
	}
	return len(c.outputStore), nil
}

// GetOutputStore implements APIConverter.
//
// Converts all objects in the outputStore to unstructured format for use by the caller.
// This method performs the final conversion step after translation, transforming the typed Kong
// resources into unstructured.Unstructured objects that can be applied to the Kubernetes cluster.
//
// The function performs the following operations:
// 1. Iterates through all objects in the outputStore (populated by translate())
// 2. Converts each typed object to unstructured format using the runtime scheme
// 3. Collects conversion errors instead of failing fast to maximize error visibility
// 4. Returns both successfully converted objects and aggregated conversion errors
//
// Error Handling Strategy:
// - Individual conversion failures are logged and collected but don't stop processing
// - Failed objects are excluded from the returned slice but processing continues
// - This provides complete error visibility rather than failing on the first conversion error
// - Returns aggregated errors using [errors.Join] for proper error chaining
//
// Parameters:
//   - ctx: The context for the conversion operation
//   - logger: Logger for structured logging with output-store-conversion phase
//
// Returns:
//   - []unstructured.Unstructured: Successfully converted objects ready for use by the caller
//   - error: Aggregated conversion errors or nil if all conversions succeeded
//
// The function prioritizes complete error visibility over fail-fast behavior, allowing
// the user to see all conversion issues at once rather than fixing them one by one.
func (c *httpRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	logger = logger.WithValues("phase", "output-store-conversion")
	log.Debug(logger, "Starting output store conversion")

	var conversionErrors []error

	// c.outputStore is already deduplicated in translate(); no need to deduplicate again here.
	objects := make([]unstructured.Unstructured, 0, len(c.outputStore))
	for _, obj := range c.outputStore {
		unstr, err := utils.ToUnstructured(obj, c.Scheme())
		if err != nil {
			conversionErr := fmt.Errorf("failed to convert %T %s to unstructured: %w", obj, obj.GetName(), err)
			conversionErrors = append(conversionErrors, conversionErr)
			log.Error(logger, err, "Failed to convert object to unstructured",
				"objectName", obj.GetName())
			continue
		}
		objects = append(objects, unstr)
	}

	// Check if any conversion errors occurred and return aggregated error.
	if len(conversionErrors) > 0 {
		log.Error(logger, nil, "Output store conversion completed with errors",
			"totalObjectsAttempted", len(c.outputStore),
			"successfulConversions", len(objects),
			"conversionErrors", len(conversionErrors))

		// Join all errors using errors.Join for better error handling.
		return objects, fmt.Errorf("output store conversion failed with %d errors: %w", len(conversionErrors), errors.Join(conversionErrors...))
	}

	log.Debug(logger, "Successfully converted all objects in output store",
		"totalObjectsConverted", len(objects))

	log.Debug(logger, "Finished output store conversion", "totalObjectsConverted", len(objects))
	return objects, nil
}

// GetOutputStoreLen returns the number of objects in the output store.
func (c *httpRouteConverter) GetOutputStoreLen(ctx context.Context, logger logr.Logger) int {
	// c.outputStore is already deduplicated in translate().
	return len(c.outputStore)
}

// GetExpectedGVKs returns the list of GroupVersionKinds that this converter expects to manage for HTTPRoute resources.
func (c *httpRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

// UpdateRootObjectStatus updates the status of the HTTPRoute by processing each ParentReference
// and setting appropriate conditions based on the Gateway's support and readiness.
//
// The function performs the following operations:
// 1. Iterates through each ParentRef in the HTTPRoute spec
// 2. Validates if the ParentRef is supported by this controller (checks Gateway and GatewayClass)
// 3. Builds and sets the "Accepted" condition for supported ParentRefs
// 4. Skips unsupported ParentRefs (wrong controller, missing Gateway/GatewayClass)
// 5. Cleans up orphaned ParentStatus entries that are no longer relevant
// 6. Updates the HTTPRoute status in the cluster if any changes were made
//
// Parameters:
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//
// Returns:
//   - updated: true if the status was modified
//   - stop: true if reconciliation should halt
//   - err: any error encountered during status update processing
//
// The function respects controller ownership and only manages ParentStatus entries
// for Gateways controlled by this controller, leaving other controllers' entries untouched.
func (c *httpRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	return route.UpdateRouteStatus(ctx, logger, c.Client, c.route, c.expectedGVKs, route.BuildResolvedRefsConditionForHTTPRoute)
}

// DesiredResourcesReady implements DesiredStateReadinessChecker. It decides whether orphan cleanup may
// proceed for this HTTPRoute.
//
// THE GATE is a single rule: every desired KongRoute must be bound, in Konnect, to its desired
// KongService — verified via the authoritative status.konnect.serviceID, NOT the route's Programmed
// condition. After a rollout repoints a route's serviceRef, the route can keep reporting Programmed=True
// against the OLD service until the Konnect controller actually reprograms it; deleting the old chain
// (KongTargets/KongUpstream/KongService) in that window drops traffic. Only the serviceID matching the
// desired service tells us the route has truly moved and the old chain is safe to remove.
//
// We deliberately do NOT gate on the Programmed condition of the desired resources: the enforce-time chain
// gating (KongService waits for its KongUpstream and KongTargets; KongRoute waits for the service)
// already builds and programs the whole chain before a route is ever repointed, so a Programmed
// check here would be redundant. The route serviceID is the only condition that actually makes deleting
// the old chain safe.
func (c *httpRouteConverter) DesiredResourcesReady(ctx context.Context, logger logr.Logger) (bool, error) {
	desired, err := c.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects for readiness check: %w", err)
	}

	// THE GATE: for each desired KongRoute, the service named in its spec.serviceRef must be the one it is
	// actually bound to in Konnect. spec.serviceRef is a name and status.konnect.serviceID is a Konnect ID,
	// so we fetch that one referenced KongService to resolve its Konnect ID and compare. The route's
	// Programmed condition is not used: it lags on the old service right after a serviceRef repoint.
	for i := range desired {
		d := &desired[i]
		if d.GetKind() != "KongRoute" {
			continue
		}

		r := &unstructured.Unstructured{}
		r.SetGroupVersionKind(d.GroupVersionKind())
		if err := c.Get(ctx, client.ObjectKeyFromObject(d), r); err != nil {
			if apierrors.IsNotFound(err) {
				// Route not applied yet: it cannot be confirmed on its desired service, so defer.
				log.Debug(logger, "Desired KongRoute not found yet, deferring orphan cleanup", "obj", client.ObjectKeyFromObject(d))
				return false, nil
			}
			return false, fmt.Errorf("failed to get KongRoute %s: %w", client.ObjectKeyFromObject(d), err)
		}

		serviceRefName, _, _ := unstructured.NestedString(r.Object, "spec", "serviceRef", "namespacedRef", "name")
		if serviceRefName == "" {
			log.Debug(logger, "KongRoute has no serviceRef, skipping readiness check", "obj", client.ObjectKeyFromObject(r))
			continue
		}
		boundServiceID, _, _ := unstructured.NestedString(r.Object, "status", "konnect", "serviceID")

		// Fetch the referenced KongService: it must be Programmed (ready) and the route must be bound to
		// its Konnect ID. spec.serviceRef is a name; the service's Konnect ID lives in its status.
		var svc configurationv1alpha1.KongService
		if err := c.Get(ctx, client.ObjectKey{Namespace: d.GetNamespace(), Name: serviceRefName}, &svc); err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug(logger, "Referenced KongService not found yet, deferring orphan cleanup", "route", client.ObjectKeyFromObject(r), "service", serviceRefName)
				return false, nil
			}
			return false, fmt.Errorf("failed to get KongService %s for route %s: %w", serviceRefName, client.ObjectKeyFromObject(r), err)
		}
		if !meta.IsStatusConditionTrue(svc.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType) {
			log.Debug(logger, "Referenced KongService not Programmed yet, deferring orphan cleanup", "route", client.ObjectKeyFromObject(r), "service", serviceRefName)
			return false, nil
		}

		var desiredServiceID string
		if svc.Status.Konnect != nil {
			desiredServiceID = svc.Status.Konnect.ID
		}
		if desiredServiceID == "" || boundServiceID != desiredServiceID {
			log.Debug(logger, "KongRoute not yet bound to its referenced KongService in Konnect, deferring orphan cleanup",
				"route", client.ObjectKeyFromObject(r),
				"service", serviceRefName,
				"boundServiceID", boundServiceID,
				"desiredServiceID", desiredServiceID)
			return false, nil
		}
	}

	log.Debug(logger, "All routes are bound to their referenced KongService in Konnect, orphan cleanup may proceed")
	return true, nil
}

// HandleOrphanedResource implements OrphanedResourceHandler.
//
// Processes orphaned resources by checking and updating hybrid-routes annotations.
// This method is called during cleanup to check if the resource passed as argument was part of the set of translated resources
// derived from the source HTTPRoute. If so, it removes the route reference from the hybrid-routes annotation and determines whether
// to update the resource and if it should be skipped from deletion.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for debugging information
//   - resource: The orphaned resource to process
//
// Returns:
//   - skipDelete: true if the resource should NOT be deleted (skip deletion), false if it should be deleted
//   - err: any error that occurred during processing
func (c *httpRouteConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error) {
	am := metadata.NewAnnotationManager(logger)
	key := client.ObjectKeyFromObject(resource)
	gvk := resource.GroupVersionKind()

	// Remove this Route from the shared hybrid-routes annotation atomically. Multiple Routes (or
	// rules) can share the same Kong resource, so a concurrent Route adding itself must not be lost
	// and the resource must not be deleted while still referenced. We re-read the live object, drop
	// our entry, and either patch with an optimistic lock (when other Routes remain) or surface the
	// validated resourceVersion so the caller can delete with an optimistic-lock precondition.
	fresh := &unstructured.Unstructured{}
	fresh.SetGroupVersionKind(gvk)
	if err := c.Get(ctx, key, fresh); err != nil {
		if apierrors.IsNotFound(err) {
			// Already gone; nothing to delete.
			return true, nil
		}
		return true, fmt.Errorf("failed to get resource: %w", err)
	}

	// If the route is not present in the hybrid-routes annotation of the Kong resource, don't touch it.
	if !am.ContainsRoute(fresh, c.route) {
		log.Trace(logger, "Route annotation not found, skipping resource", "kind", fresh.GetKind(), "obj", key)
		return true, nil
	}

	base := fresh.DeepCopy()
	am.RemoveRouteFromAnnotation(fresh, c.route)

	// If other Routes are still present in the annotation, we just need to update the resource.
	if len(am.GetRoutesWithKind(fresh, "HTTPRoute")) > 0 {
		log.Debug(logger, "Updating hybrid-routes annotation", "kind", fresh.GetKind(), "obj", key)
		if err := c.Patch(ctx, fresh, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
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

// translate converts the HTTPRoute to Kong resources and stores them in outputStore.
//
// The function performs the following operations:
// 1. Retrieves and validates supported parent references (Gateways).
// 2. For each parent reference and rule combination, creates Kong resources:
//   - KongUpstream: Manages backend service endpoints.
//   - KongTarget: Individual backend targets with weight calculations.
//   - KongService: Kong service configuration pointing to upstream.
//   - KongRoute: Route matching and routing configuration.
//   - KongPlugin: Filter-based plugins for request/response processing.
//   - KongPluginBinding: Binds plugins to specific routes.
//
// 3. Collects translation errors instead of failing fast to maximize error visibility.
// 4. Returns aggregated errors using [errors.Join] for proper error chaining.
//
// Error Handling Strategy:
// - Individual resource creation failures are logged and collected but don't stop processing.
// - Failed resources are skipped and not created, but translation continues for remaining resources.
// - This provides complete error visibility rather than failing fast on the first error.
// - Only critical failures (like parent reference resolution) cause immediate return.
//
// Parameters:
//   - ctx: The context for API calls and cancellation.
//   - logger: Logger for structured logging with httproute-translate phase.
//
// Returns:
//   - error: Aggregated translation errors or nil if successful.
//
// The function prioritizes complete error visibility over fail-fast behavior, allowing
// users to see all translation issues at once rather than fixing them one by one.
func (c *httpRouteConverter) translate(ctx context.Context, logger logr.Logger) error {
	logger = logger.WithValues("phase", "httproute-translate")
	log.Debug(logger, "Starting HTTPRoute translation")

	var translationErrors []error

	supportedParentRefs, err := getHybridGatewayParents(ctx, logger, c.Client, c.route)
	if err != nil {
		log.Error(logger, err, "Failed to get supported parent references")
		return err
	}
	if len(supportedParentRefs) == 0 {
		log.Info(logger, "No supported parent references found, skipping translation")
		return nil
	}

	log.Debug(logger, "Found supported parent references",
		"parentRefCount", len(supportedParentRefs))

	for _, pRefData := range supportedParentRefs {
		pRef := pRefData.parentRef
		cp := pRefData.cpRef
		hostnames := pRefData.hostnames
		var namingParentRef *gwtypes.ParentReference
		if len(supportedParentRefs) > 1 {
			namingParentRef = &pRef
		}

		log.Debug(logger, "Processing parent reference",
			"parentRef", pRef,
			"hostnames", hostnames,
			"ruleCount", len(c.route.Spec.Rules))

		for ruleIndex, rule := range c.route.Spec.Rules {
			log.Debug(logger, "Processing rule",
				"ruleIndex", ruleIndex,
				"backendRefCount", len(rule.BackendRefs),
				"matchCount", len(rule.Matches),
				"filterCount", len(rule.Filters))

			upstreamName := namegen.NewKongUpstreamNameForHTTPRouteRule(c.route, cp, rule)

			// Build the KongTarget resources before the service so fallback services for
			// invalid backends can use a route-scoped name and avoid colliding with normal backend services.
			targets, err := target.TargetsForBackendRefs(
				ctx,
				logger.WithValues("upstream", upstreamName),
				c.Client,
				c.route,
				rule.BackendRefs,
				&pRef,
				upstreamName,
				c.fqdnMode,
				c.clusterDomain,
			)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongTarget resources for rule, skipping rule",
					"upstream", upstreamName,
					"backendRefs", rule.BackendRefs,
					"parentRef", pRef)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongTarget resources for upstream %s: %w", upstreamName, err))
				continue
			}
			log.Debug(logger, "Successfully translated KongTarget resources", "upstream", upstreamName, "targetCount", len(targets))

			serviceNameOverride := ""
			if len(rule.BackendRefs) > 0 && len(targets) == 0 {
				serviceNameOverride = namegen.NewKongServiceNameForHTTPRouteRuleBackendNotFound(c.route, cp, rule)
			}

			// Build the KongService resource (and optionally a KongCertificate + KongReferenceGrant for client-cert).
			servicePtr, certPtr, grantPtr, err := service.ServiceForRuleWithName(ctx, logger, c.Client, c.route, rule, &pRef, cp, upstreamName, serviceNameOverride)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongService resource, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef,
					"upstream", upstreamName)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongService for rule: %w", err))
				continue
			}
			serviceName := servicePtr.Name

			// Build one KongRoute per match in the rule.
			// Gateway API semantics require OR across matches within a rule
			// and AND within a single match. Generating a route per match
			// preserves the OR semantics for Hybrid Gateway.
			routes, err := kongroute.RoutesForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp, namingParentRef, serviceName, hostnames)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongRoute resources for rule, skipping rule",
					"ruleIndex", ruleIndex,
					"service", serviceName,
					"hostnames", hostnames)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongRoutes for rule %d: %w", ruleIndex, err))
				continue
			}
			// Build the KongPlugin and KongPluginBinding resources.
			log.Debug(logger, "Processing filters for rule",
				"filterCount", len(rule.Filters))
			filterOutputs := make([]client.Object, 0)

			for _, filter := range rule.Filters {
				plugins, err := plugin.PluginsForFilter(ctx, logger, c.Client, c.route, rule, filter, &pRef)
				if err != nil {
					log.Error(logger, err, "Failed to translate KongPlugin resource, skipping filter",
						"filter", filter.Type)
					translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongPlugin for filter: %w", err))
					continue
				}

				for i := range plugins {
					pluginObj := &plugins[i]
					pluginName := pluginObj.Name
					filterOutputs = append(filterOutputs, pluginObj)
					// Create a KongPluginBinding to bind the KongPlugin to each KongRoute generated for the rule.
					for _, r := range routes {
						bindingPtr, err := pluginbinding.BindingForPluginAndRoute(
							ctx,
							logger,
							c.Client,
							c.route,
							&pRef,
							cp,
							pluginName,
							r.Name,
						)
						if err != nil {
							log.Error(logger, err, "Failed to build KongPluginBinding resource, skipping binding",
								"plugin", pluginName,
								"kongRoute", r.Name)
							translationErrors = append(translationErrors, fmt.Errorf("failed to build KongPluginBinding for plugin %s: %w", pluginName, err))
							continue
						}
						bindingName := bindingPtr.Name
						filterOutputs = append(filterOutputs, bindingPtr)

						log.Debug(logger, "Successfully translated KongPlugin and KongPluginBinding resources",
							"plugin", pluginName,
							"binding", bindingName,
							"route", r.Name)
					}
				}
			}

			upstreamPtr, err := upstream.UpstreamForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongUpstream resource for rule, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongUpstream resource: %w", err))
				continue
			}

			ruleOutputs := []client.Object{upstreamPtr}
			log.Debug(logger, "Successfully translated KongUpstream resource", "upstream", upstreamName)

			// Append KongReferenceGrant before KongCertificate so the grant exists when the cert is applied.
			if grantPtr != nil {
				ruleOutputs = append(ruleOutputs, grantPtr)
				log.Debug(logger, "Successfully translated KongReferenceGrant resource", "grant", grantPtr.Name)
			}
			if certPtr != nil {
				ruleOutputs = append(ruleOutputs, certPtr)
				log.Debug(logger, "Successfully translated KongCertificate resource", "cert", certPtr.Name)
			}
			ruleOutputs = append(ruleOutputs, servicePtr)
			log.Debug(logger, "Successfully translated KongService resource", "service", serviceName)
			for _, r := range routes {
				routeName := r.Name
				ruleOutputs = append(ruleOutputs, r)
				log.Debug(logger, "Successfully translated KongRoute resource", "route", routeName)
			}
			ruleOutputs = append(ruleOutputs, filterOutputs...)

			if len(rule.BackendRefs) > 0 && len(targets) == 0 {
				terminationPlugin, err := plugin.RequestTerminationForBackendNotFound(
					ctx,
					logger,
					c.Client,
					c.route,
					&pRef,
					serviceName,
				)
				if err != nil {
					log.Error(logger, err, "Failed to translate request-termination plugin for backend-less rule",
						"service", serviceName,
						"backendRefs", rule.BackendRefs)
					translationErrors = append(translationErrors, fmt.Errorf("failed to translate request-termination plugin for service %s: %w", serviceName, err))
					continue
				}

				bindingPtr, err := pluginbinding.BindingForPluginAndService(
					ctx,
					logger,
					c.Client,
					c.route,
					&pRef,
					cp,
					terminationPlugin.Name,
					serviceName,
				)
				if err != nil {
					log.Error(logger, err, "Failed to bind request-termination plugin to service",
						"service", serviceName,
						"plugin", terminationPlugin.Name)
					translationErrors = append(translationErrors, fmt.Errorf("failed to bind request-termination plugin %s to service %s: %w", terminationPlugin.Name, serviceName, err))
					continue
				}
				ruleOutputs = append(ruleOutputs, terminationPlugin, bindingPtr)
			}

			for i := range targets {
				ruleOutputs = append(ruleOutputs, &targets[i])
			}

			c.outputStore = append(c.outputStore, ruleOutputs...)
		}
	}

	c.outputStore = deduplicateOutputStore(c.outputStore)

	// Check if any translation errors occurred
	if len(translationErrors) > 0 {
		log.Error(logger, nil, "HTTPRoute translation completed with errors",
			"totalResourcesCreated", len(c.outputStore),
			"errorCount", len(translationErrors))

		// Join all errors using errors.Join for better error handling
		return fmt.Errorf("translation failed with %d errors: %w", len(translationErrors), errors.Join(translationErrors...))
	}

	log.Debug(logger, "Successfully completed HTTPRoute translation",
		"totalResourcesCreated", len(c.outputStore))

	return nil
}

type hybridGatewayParent struct {
	parentRef gwtypes.ParentReference
	cpRef     *commonv1alpha1.ControlPlaneRef
	hostnames []string
}
