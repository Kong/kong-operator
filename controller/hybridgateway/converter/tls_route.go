package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/kongroute"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/service"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/target"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/upstream"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

var _ APIConverter[gwtypes.TLSRoute] = &tlsRouteConverter{}

type tlsRouteConverter struct {
	client.Client

	route         *gwtypes.TLSRoute
	outputStore   []client.Object
	expectedGVKs  []schema.GroupVersionKind
	fqdnMode      bool
	clusterDomain string
}

func newTLSRouteConverter(tlsRoute *gwtypes.TLSRoute, cl client.Client, fqdnMode bool, clusterDomain string) APIConverter[gwtypes.TLSRoute] {
	return &tlsRouteConverter{
		Client:      cl,
		route:       tlsRoute,
		outputStore: []client.Object{},
		expectedGVKs: []schema.GroupVersionKind{
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongRoute"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongTarget"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongService"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongCertificate"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongReferenceGrant"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongUpstream"},
		},
		fqdnMode:      fqdnMode,
		clusterDomain: clusterDomain,
	}
}

// GetExpectedGVKs implements the APIConverter interface.
// It returns the list of GroupVersionKinds that this converter expects to manage for TLSRoute resources.
func (c *tlsRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

// GetRootObject implements the APIConverter interface.
// It returns the TLSRoute resource that the converter is managing.
func (c *tlsRouteConverter) GetRootObject() gwtypes.TLSRoute {
	return *c.route
}

// UpdateRootObjectStatus implements the APIConverter interface.
// It updates the status of the TLSRoute by processing each ParentReference
// and setting appropriate conditions based on the Gateway's support and readiness.
//
// The return values:
// - updated is true if the status is modified;
// - stop is true if the TLSRoute is not ready for the next round of reconciliation;
// - err is the error happened (if there is) in processing.
func (c *tlsRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	return route.UpdateRouteStatus(ctx, logger, c.Client, c.route, c.expectedGVKs, route.BuildResolvedRefsConditionForTLSRoute)
}

// GetOutputStore implements APIConverter.
//
// Converts all objects in the outputStore to unstructured format for use by the caller.
// It outputs all resources generated from the `Translate` method that translate the TLSRoute to entities that can be managed in Konnect.
// A non-nil error is returned if there are errors in the translation.
func (c *tlsRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
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

// HandleOrphanedResource implements OrphanedResourceHandler.
//
// Processes orphaned resources by checking and updating hybrid-routes annotations.
//
// Returns:
//   - skipDelete: true if the resource should NOT be deleted (skip deletion), false if it should be deleted
//   - err: any error that occurred during processing
func (c *tlsRouteConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error) {
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
	if len(am.GetRoutesWithKind(fresh, "TLSRoute")) > 0 {
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

// Translate implements APIConverter.
//
// Performs the complete translation of an TLSRoute resource into Kong-specific resources.
// This is the main entry point for the conversion process, delegating to the internal
// translate() method for the actual implementation.
//
// Returns:
//   - int: Number of Kong resources created during translation
//   - error: Aggregated translation errors or nil if successful
func (c *tlsRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	if err := c.translate(ctx, logger); err != nil {
		return 0, err
	}
	return len(c.outputStore), nil
}

func (c *tlsRouteConverter) translate(ctx context.Context, logger logr.Logger) error {

	logger = logger.WithValues("phase", "tlsroute-translate")
	log.Debug(logger, "Starting TLSRoute translation")

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
		)

		for ruleIndex, rule := range c.route.Spec.Rules {
			log.Debug(logger, "Processing rule",
				"ruleIndex", ruleIndex,
				"backendRefCount", len(rule.BackendRefs),
			)

			// Build the KongUpstream resource.
			upstreamPtr, err := upstream.UpstreamForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongUpstream resource for rule, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongUpstream resource: %w", err))
				continue
			}
			upstreamName := upstreamPtr.Name
			c.outputStore = append(c.outputStore, upstreamPtr)
			log.Debug(logger, "Successfully translated KongUpstream resource", "upstream", upstreamName)

			// Build the KongService resource (and optionally a KongCertificate + KongReferenceGrant for client-cert).
			servicePtr, certPtr, grantPtr, err := service.ServiceForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp, upstreamName)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongService resource, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef,
					"upstream", upstreamName)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongService for rule: %w", err))
				continue
			}
			// Append KongReferenceGrant before KongCertificate so the grant exists when the cert is applied.
			if grantPtr != nil {
				c.outputStore = append(c.outputStore, grantPtr)
				log.Debug(logger, "Successfully translated KongReferenceGrant resource", "grant", grantPtr.Name)
			}
			if certPtr != nil {
				c.outputStore = append(c.outputStore, certPtr)
				log.Debug(logger, "Successfully translated KongCertificate resource", "cert", certPtr.Name)
			}
			serviceName := servicePtr.Name
			c.outputStore = append(c.outputStore, servicePtr)
			log.Debug(logger, "Successfully translated KongService resource", "service", serviceName)

			// Build the KongRoute resource.
			routes, err := kongroute.RoutesForRule(ctx, logger, c.Client, c.route, rule, ruleIndex, &pRef, cp, namingParentRef, serviceName, hostnames)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongRoute resource, skipping rule",
					"service", serviceName,
					"hostnames", hostnames)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongRoute for rule: %w", err))
				continue
			}
			for _, routePtr := range routes {
				routeName := routePtr.Name
				c.outputStore = append(c.outputStore, routePtr)
				log.Debug(logger, "Successfully translated KongRoute resource",
					"route", routeName)
			}

			// Build the KongTarget resources.
			// Leave them as the last step since we want everything fully configured before enabling the traffic to the backends.
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
			log.Debug(logger, "Successfully translated KongTarget resources",
				"upstream", upstreamName,
				"targetCount", len(targets))
			for _, tgt := range targets {
				c.outputStore = append(c.outputStore, &tgt)
			}

		}
	}

	c.outputStore = deduplicateOutputStore(c.outputStore)

	// Check if any translation errors occurred
	if len(translationErrors) > 0 {
		log.Error(logger, nil, "TLSRoute translation completed with errors",
			"totalResourcesCreated", len(c.outputStore),
			"errorCount", len(translationErrors))

		// Join all errors using errors.Join for better error handling
		return fmt.Errorf("translation failed with %d errors: %w", len(translationErrors), errors.Join(translationErrors...))
	}

	log.Debug(logger, "Successfully completed TLSRoute translation",
		"totalResourcesCreated", len(c.outputStore))

	return nil
}
