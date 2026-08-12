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
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

var _ APIConverter[gwtypes.GRPCRoute] = &grpcRouteConverter{}
var _ DesiredStateReadinessChecker = &grpcRouteConverter{}
var _ OrphanedResourceHandler = &grpcRouteConverter{}

type grpcRouteConverter struct {
	client.Client

	route         *gwtypes.GRPCRoute
	outputStore   []client.Object
	expectedGVKs  []schema.GroupVersionKind
	fqdnMode      bool
	clusterDomain string
}

// HandleOrphanedResource removes this GRPCRoute from an orphaned resource's hybrid-routes annotation.
func (c *grpcRouteConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error) {
	am := metadata.NewAnnotationManager(logger)
	key := client.ObjectKeyFromObject(resource)
	gvk := resource.GroupVersionKind()

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
	if len(am.GetRoutesWithKind(fresh, "GRPCRoute")) > 0 {
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

// DesiredResourcesReady implements DesiredStateReadinessChecker. It decides whether orphan cleanup
// may proceed for this GRPCRoute. Mirrors httpRouteConverter.DesiredResourcesReady's gate: see that
// method's doc comment for the full rationale (serviceID-based, not Programmed-based, gating).
func (c *grpcRouteConverter) DesiredResourcesReady(ctx context.Context, logger logr.Logger) (bool, error) {
	desired, err := c.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects for readiness check: %w", err)
	}

	for i := range desired {
		d := &desired[i]
		if d.GetKind() != "KongRoute" {
			continue
		}

		r := &unstructured.Unstructured{}
		r.SetGroupVersionKind(d.GroupVersionKind())
		if err := c.Get(ctx, client.ObjectKeyFromObject(d), r); err != nil {
			if apierrors.IsNotFound(err) {
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

// GetExpectedGVKs returns the ordered list of Kong resource GVKs managed for a GRPCRoute.
func (c *grpcRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

// Translate converts the GRPCRoute into desired Kong resources and stores them in the output store.
func (c *grpcRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	if err := c.translate(ctx, logger); err != nil {
		return 0, err
	}
	return len(c.outputStore), nil
}

// GetRootObject returns a copy of the GRPCRoute managed by this converter.
func (c *grpcRouteConverter) GetRootObject() gwtypes.GRPCRoute {
	return *c.route
}

// GetOutputStore converts the translated Kong resources into unstructured objects.
func (c *grpcRouteConverter) GetOutputStore(_ context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	return convertOutputStoreToUnstructured(logger, c.Scheme(), c.outputStore)
}

// UpdateRootObjectStatus reconciles the GRPCRoute status conditions for its parent references.
func (c *grpcRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	return route.UpdateRouteStatus(ctx, logger, c.Client, c.route, c.expectedGVKs, route.BuildResolvedRefsConditionForGRPCRoute)
}

func newGRPCRouteConverter(grpcRoute *gwtypes.GRPCRoute, cl client.Client, fqdnMode bool, clusterDomain string) APIConverter[gwtypes.GRPCRoute] {
	return &grpcRouteConverter{
		Client:        cl,
		route:         grpcRoute,
		outputStore:   []client.Object{},
		fqdnMode:      fqdnMode,
		clusterDomain: clusterDomain,
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

func (c *grpcRouteConverter) translate(ctx context.Context, logger logr.Logger) error {
	logger = logger.WithValues("phase", "grpcroute-translate")
	log.Debug(logger, "Starting GRPCRoute translation")

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

			upstreamName := namegen.NewKongUpstreamNameForGRPCRouteRule(c.route, cp, rule)

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
				serviceNameOverride = namegen.NewKongServiceNameForGRPCRouteRuleBackendNotFound(c.route, cp, rule)
			}

			servicePtr, certPtr, grantPtr, err := service.ServiceForRuleWithName(ctx, logger, c.Client, c.route, rule, &pRef, cp, upstreamName, serviceNameOverride)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongService resource, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef,
					"upstream", upstreamName)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongService for rule: %w", err))
				continue
			}
			serviceName := servicePtr.Name

			routes, err := kongroute.RoutesForRule(ctx, logger, c.Client, c.route, rule, ruleIndex, &pRef, cp, namingParentRef, serviceName, hostnames)
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

			// Translate the rule's filters into KongPlugins. RequestMirror is not yet supported
			// (see translateGRPCFromFilter); filters that map to the same Kong plugin type are
			// merged into a single KongPlugin so a route never gets two plugins of the same type
			// bound to it (Kong's unique-plugin-per-entity constraint).
			plugins, err := plugin.GRPCPluginsForRule(ctx, logger, c.Client, c.route, rule, &pRef)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongPlugin resources for rule, skipping plugins",
					"ruleIndex", ruleIndex)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongPlugins for rule %d: %w", ruleIndex, err))
				plugins = nil
			}

			for i := range plugins {
				pluginObj := &plugins[i]
				pluginName := pluginObj.Name
				filterOutputs = append(filterOutputs, pluginObj)
				// Create a KongPluginBinding to bind the KongPlugin to each KongRoute generated for the rule.
				for _, r := range routes {
					bindingPtr, err := pluginbinding.BindingForGRPCPluginAndRoute(
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

			// Per Gateway API semantics, a rule that resolves to no valid backend must not
			// silently succeed. GRPCRoute has no filter analogous to HTTPRoute's RequestRedirect
			// that produces a response without a backend, so unlike HTTPRoute's translate() this
			// safety net applies unconditionally whenever a rule with backendRefs resolves to no
			// targets.
			if len(targets) == 0 {
				terminationPlugin, err := plugin.GRPCRequestTerminationForBackendNotFound(
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

				bindingPtr, err := pluginbinding.BindingForGRPCPluginAndService(
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

	if len(translationErrors) > 0 {
		log.Error(logger, nil, "GRPCRoute translation completed with errors",
			"totalResourcesCreated", len(c.outputStore),
			"errorCount", len(translationErrors))

		return fmt.Errorf("translation failed with %d errors: %w", len(translationErrors), errors.Join(translationErrors...))
	}

	log.Debug(logger, "Successfully completed GRPCRoute translation",
		"totalResourcesCreated", len(c.outputStore))

	return nil
}
