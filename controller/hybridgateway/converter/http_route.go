package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	hybridgatewayerrors "github.com/kong/kong-operator/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/target"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

var _ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	route                 *gwtypes.HTTPRoute
	outputStore           []client.Object
	expectedGVKs          []schema.GroupVersionKind
	referenceGrantEnabled bool
	fqdnMode              bool
	clusterDomain         string
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func newHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client, referenceGrantEnabled bool, fqdnMode bool, clusterDomain string) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:                cl,
		outputStore:           []client.Object{},
		route:                 httpRoute,
		referenceGrantEnabled: referenceGrantEnabled,
		fqdnMode:              fqdnMode,
		clusterDomain:         clusterDomain,
		expectedGVKs: []schema.GroupVersionKind{
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongRoute"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongService"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongUpstream"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongTarget"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1.GroupVersion.Version, Kind: "KongPlugin"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongPluginBinding"},
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
//   - error: Aggregated translation errors or nil if successful
func (c *httpRouteConverter) Translate(ctx context.Context, logger logr.Logger) error {
	return c.translate(ctx, logger)
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
// - Returns aggregated errors using errors.Join for proper error chaining
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
//   - bool: true if the status was updated, false if no changes were made
//   - error: Any error that occurred during status processing
//
// The function respects controller ownership and only manages ParentStatus entries
// for Gateways controlled by this controller, leaving other controllers' entries untouched.
func (c *httpRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (bool, error) {
	logger = logger.WithValues("phase", "httproute-status")
	log.Debug(logger, "Starting UpdateRootObjectStatus")
	updated := false

	// First, build the resolvedRefs conditons for the HTTPRoute since it is the same for all ParentRefs.
	log.Debug(logger, "Building ResolvedRefs condition for HTTPRoute")
	resolvedRefsCond, err := route.BuildResolvedRefsCondition(ctx, logger, c.Client, c.route, c.referenceGrantEnabled)
	if err != nil {
		return false, fmt.Errorf("failed to build resolvedRefs condition for HTTPRoute %s: %w", c.route.Name, err)
	}

	// For each parentRef in the HTTPRoute, build the conditions and set them in the status.
	for _, pRef := range c.route.Spec.ParentRefs {
		log.Debug(logger, "Processing ParentReference", "parentRef", pRef)
		// Check if the parentRef belongs to a Gateway managed by us.
		gateway, err := refs.GetSupportedGatewayForParentRef(ctx, logger, c.Client, pRef, c.route.Namespace)
		if err != nil {
			if errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound) ||
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController) ||
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound) {
				// If the gateway is not managed by us or not found we skip setting conditions.
				log.Debug(logger, "Skipping status update for unsupported or non-existent Gateway", "parentRef", pRef)
				if route.RemoveStatusForParentRef(logger, c.route, pRef, vars.ControllerName()) {
					// If we removed the status, we need to mark the update as true.
					log.Debug(logger, "Removed ParentStatus for unsupported ParentReference", "parentRef", pRef)
					updated = true
				}
				continue
			} else {
				log.Error(logger, err, "Failed to get supported gateway for ParentReference", "parentRef", pRef)
				return false, fmt.Errorf("failed to get supported gateway for parentRef %s: %w", pRef.Name, err)
			}
		}

		log.Debug(logger, "Building Accepted condition", "parentRef", pRef, "gateway", gateway.Name)
		acceptedCondition, err := route.BuildAcceptedCondition(ctx, logger, c.Client, gateway, c.route, pRef)
		if err != nil {
			return false, fmt.Errorf("failed to build accepted condition for parentRef %s: %w", pRef.Name, err)
		}

		log.Debug(logger, "Building Programmed conditions", "parentRef", pRef, "gateway", gateway.Name)
		programmedConditions, err := route.BuildProgrammedCondition(ctx, logger, c.Client, c.route, pRef, c.expectedGVKs)
		if err != nil {
			return false, fmt.Errorf("failed to build programmed condition for parentRef %s: %w", pRef.Name, err)
		}

		// Combine all conditions.
		programmedConditions = append(programmedConditions, *acceptedCondition, *resolvedRefsCond)

		log.Debug(logger, "Setting status conditions", "parentRef", pRef, "conditionsCount", len(programmedConditions))
		if route.SetStatusConditions(c.route, pRef, vars.ControllerName(), programmedConditions...) {
			log.Debug(logger, "Status conditions updated for ParentReference", "parentRef", pRef)
			updated = true
		}
	}

	log.Debug(logger, "Cleaning up orphaned ParentStatus entries")
	if route.CleanupOrphanedParentStatus(logger, c.route, vars.ControllerName()) {
		log.Debug(logger, "Orphaned ParentStatus entries cleaned up")
		updated = true
	}

	// Update the status in the cluster if there are changes.
	if updated {
		log.Debug(logger, "Updating HTTPRoute status in cluster", "status", c.route.Status)
		if err := c.Status().Update(ctx, c.route); err != nil {
			log.Error(logger, err, "Failed to update HTTPRoute status in cluster")
			return false, fmt.Errorf("failed to update HTTPRoute status: %w", err)
		}
	} else {
		log.Debug(logger, "No status update required for HTTPRoute")
	}

	log.Debug(logger, "Finished UpdateRootObjectStatus", "updated", updated)
	return updated, nil
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
// 4. Returns aggregated errors using errors.Join for proper error chaining.
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

	supportedParentRefs, err := c.getHybridGatewayParents(ctx, logger)
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

	httpRouteName := c.route.Namespace + "-" + c.route.Name

	for _, pRefData := range supportedParentRefs {
		pRef := pRefData.parentRef
		cp := pRefData.cpRef
		hostnames := pRefData.hostnames
		cpRefName := "cp" + utils.Hash32(cp)

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
			// Build the KongUpstream resource.
			upstreamName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.BackendRefs)).String()
			log.Trace(logger, "Building KongUpstream resource",
				"upstream", upstreamName,
				"controlPlane", cp.KonnectNamespacedRef)

			upstream, err := builder.NewKongUpstream().
				WithName(upstreamName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, &pRef).
				WithAnnotations(c.route, &pRef).
				WithSpecName(upstreamName).
				WithControlPlaneRef(*cp).
				WithOwner(c.route).Build()
			if err != nil {
				log.Error(logger, err, "Failed to build KongUpstream resource, skipping rule",
					"upstream", upstreamName,
					"controlPlane", cp.KonnectNamespacedRef)
				translationErrors = append(translationErrors, fmt.Errorf("failed to build KongUpstream %s: %w", upstreamName, err))
				continue
			}
			c.outputStore = append(c.outputStore, &upstream)
			log.Debug(logger, "Successfully built KongUpstream resource",
				"upstream", upstreamName)

			// Build the KongTarget resources using the new rule-based approach.
			targets, err := target.TargetsForBackendRefs(
				ctx,
				logger.WithValues("upstream", upstreamName),
				c.Client,
				c.route,
				rule.BackendRefs,
				&pRef,
				upstreamName,
				c.referenceGrantEnabled,
				c.fqdnMode,
				c.clusterDomain,
			)
			if err != nil {
				log.Error(logger, err, "Failed to create KongTarget resources for rule, skipping rule",
					"upstream", upstreamName,
					"backendRefs", rule.BackendRefs,
					"parentRef", pRef)
				translationErrors = append(translationErrors, fmt.Errorf("failed to create KongTarget resources for upstream %s: %w", upstreamName, err))
				continue
			}
			log.Debug(logger, "Successfully created KongTarget resources",
				"upstream", upstreamName,
				"targetCount", len(targets))
			for _, tgt := range targets {
				c.outputStore = append(c.outputStore, &tgt)
			}

			// Build the KongService resource.
			serviceName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.BackendRefs)).String()
			log.Trace(logger, "Building KongService resource",
				"service", serviceName,
				"upstream", upstreamName)

			service, err := builder.NewKongService().
				WithName(serviceName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, &pRef).
				WithAnnotations(c.route, &pRef).
				WithSpecName(serviceName).
				WithSpecHost(upstreamName).
				WithControlPlaneRef(*cp).
				WithOwner(c.route).Build()
			if err != nil {
				log.Error(logger, err, "Failed to build KongService resource, skipping rule",
					"service", serviceName,
					"upstream", upstreamName)
				translationErrors = append(translationErrors, fmt.Errorf("failed to build KongService %s: %w", serviceName, err))
				continue
			}
			c.outputStore = append(c.outputStore, &service)
			log.Debug(logger, "Successfully built KongService resource",
				"service", serviceName)

			// Build the kong route resource.
			routeName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.Matches)).String()
			log.Debug(logger, "Building KongRoute resource",
				"kongRoute", routeName,
				"service", serviceName,
				"hostnames", hostnames,
				"matchCount", len(rule.Matches))

			routeBuilder := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, &pRef).
				WithAnnotations(c.route, &pRef).
				WithSpecName(routeName).
				WithHosts(hostnames).
				WithStripPath(metadata.ExtractStripPath(c.route.Annotations)).
				WithKongService(serviceName).
				WithOwner(c.route)
			for _, match := range rule.Matches {
				routeBuilder = routeBuilder.WithHTTPRouteMatch(match)
			}
			route, err := routeBuilder.Build()
			if err != nil {
				log.Error(logger, err, "Failed to build KongRoute resource, skipping rule",
					"kongRoute", routeName,
					"service", serviceName,
					"hostnames", hostnames)
				translationErrors = append(translationErrors, fmt.Errorf("failed to build KongRoute %s: %w", routeName, err))
				continue
			}
			c.outputStore = append(c.outputStore, &route)
			log.Debug(logger, "Successfully built KongRoute resource",
				"kongRoute", routeName)

			// Build the kong plugin and kong plugin binding resources.
			log.Debug(logger, "Processing filters for rule",
				"kongRoute", routeName,
				"filterCount", len(rule.Filters))

			for _, filter := range rule.Filters {
				filterHash := utils.Hash32(filter)
				pluginName := namegen.NewName(httpRouteName, cpRefName, filterHash).String()

				log.Trace(logger, "Building KongPlugin resource",
					"plugin", pluginName,
					"filterType", filter.Type)

				plugin, err := builder.NewKongPlugin().
					WithName(pluginName).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route, &pRef).
					WithAnnotations(c.route, &pRef).
					WithFilter(filter).
					WithOwner(c.route).Build()
				if err != nil {
					log.Error(logger, err, "Failed to build KongPlugin resource, skipping filter",
						"plugin", pluginName,
						"filterType", filter.Type)
					translationErrors = append(translationErrors, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err))
					continue
				}
				c.outputStore = append(c.outputStore, &plugin)

				// Create a KongPluginBinding to bind the KongPlugin to each rule match.
				bindingName := routeName + "." + filterHash
				log.Trace(logger, "Building KongPluginBinding resource",
					"binding", bindingName,
					"plugin", pluginName,
					"kongRoute", routeName)

				binding, err := builder.NewKongPluginBinding().
					WithName(bindingName).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route, &pRef).
					WithAnnotations(c.route, &pRef).
					WithPluginRef(pluginName).
					WithControlPlaneRef(*cp).
					WithOwner(c.route).
					WithRouteRef(routeName).Build()
				if err != nil {
					log.Error(logger, err, "Failed to build KongPluginBinding resource, skipping binding",
						"binding", bindingName,
						"plugin", pluginName,
						"kongRoute", routeName)
					translationErrors = append(translationErrors, fmt.Errorf("failed to build KongPluginBinding %s: %w", bindingName, err))
					continue
				}
				c.outputStore = append(c.outputStore, &binding)

				log.Trace(logger, "Successfully built KongPlugin and KongPluginBinding resources",
					"plugin", pluginName,
					"binding", bindingName)
			}
		}
	}

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

// getHybridGatewayParents returns parent references that are supported by this controller.
func (c *httpRouteConverter) getHybridGatewayParents(ctx context.Context, logger logr.Logger) ([]hybridGatewayParent, error) {
	log.Debug(logger, "Getting hybrid gateway parents", "parentRefCount", len(c.route.Spec.ParentRefs))

	result := []hybridGatewayParent{}
	for i, pRef := range c.route.Spec.ParentRefs {
		log.Debug(logger, "Processing parent reference", "index", i, "parentRef", pRef)

		cp, err := refs.GetControlPlaneRefByParentRef(ctx, logger, c.Client, c.route, pRef)
		if err != nil {
			switch {
			case errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController),
				errors.Is(err, hybridgatewayerrors.ErrKonnectExtensionCrossNamespaceReference):
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

		hostnames, err := c.getHostnamesByParentRef(ctx, logger, pRef)
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

// getHostnamesByParentRef returns the hostnames that match between the HTTPRoute and the Gateway listeners.
func (c *httpRouteConverter) getHostnamesByParentRef(ctx context.Context, logger logr.Logger, pRef gwtypes.ParentReference) ([]string, error) {
	logger = logger.WithValues("parentRef", pRef.Name)
	log.Debug(logger, "Getting hostnames for ParentRef")

	var err error
	var hostnames []string

	listeners, err := refs.GetListenersByParentRef(ctx, c.Client, c.route, pRef)
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
			if listener.Port != lo.FromPtr(pRef.Port) {
				// This listener doesn't match the port reference, skip it
				continue
			}
		}

		// If the listener has no hostname, it means it accepts all HTTPRoute hostnames.
		// No need to do further checks.
		if listener.Hostname == nil || *listener.Hostname == "" {
			log.Debug(logger, "Listener accepts all hostnames", "listener", listener.Name)
			hostnames = []string{}
			for _, host := range c.route.Spec.Hostnames {
				hostnames = append(hostnames, string(host))
			}
			log.Debug(logger, "Returning all HTTPRoute hostnames", "hostnames", hostnames)
			return hostnames, nil
		}

		// Handle wildcard hostnames - get intersection
		log.Debug(logger, "Processing listener with hostname", "listener", listener.Name, "listenerHostname", *listener.Hostname)
		for _, host := range c.route.Spec.Hostnames {
			routeHostname := string(host)
			if intersection := utils.HostnameIntersection(string(*listener.Hostname), routeHostname); intersection != "" {
				log.Trace(logger, "Found hostname intersection", "listenerHostname", *listener.Hostname, "routeHostname", routeHostname, "intersection", intersection)
				hostnames = append(hostnames, intersection)
			}
		}
	}

	log.Debug(logger, "Finished processing hostnames", "finalHostnames", hostnames)
	return hostnames, nil
}
