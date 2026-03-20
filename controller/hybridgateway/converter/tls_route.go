package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/kongroute"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/service"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/target"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/upstream"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/vars"
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
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongUpstream"},
		},
		fqdnMode:      fqdnMode,
		clusterDomain: clusterDomain,
	}
}

func (c *tlsRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

func (c *tlsRouteConverter) GetRootObject() gwtypes.TLSRoute {
	return *c.route
}

func (c *tlsRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	logger = logger.WithValues("phase", "tlsroute-status")
	log.Debug(logger, "Starting UpdateRootObjectStatus")

	// First, build the resolvedRefs conditons for the TLSRoute since it is the same for all ParentRefs.
	log.Debug(logger, "Building ResolvedRefs condition for TLSRoute")
	resolvedRefsCond, err := route.BuildResolvedRefsCondition(ctx, logger, c.Client, c.route)
	if err != nil {
		return false, stop, fmt.Errorf("failed to build resolvedRefs condition for TLSRoute %s: %w", c.route.Name, err)
	}

	// For each parentRef in the TLSRoute, build the conditions and set them in the status.
	for _, pRef := range c.route.Spec.ParentRefs {
		log.Debug(logger, "Processing ParentReference", "parentRef", pRef)
		// Check if the parentRef belongs to a Gateway managed by us.
		gateway, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, c.Client, pRef, c.route.Namespace)
		if err != nil {
			switch {
			case errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup):
				// If the gateway is not managed by us or not found we skip setting conditions.
				log.Debug(logger, "Skipping status update Gateway", "parentRef", pRef, "reason", err)
				if route.RemoveStatusForParentRef(logger, c.route, pRef, vars.ControllerName()) {
					// If we removed the status, we need to mark the update as true.
					log.Debug(logger, "Removed ParentStatus for unsupported ParentReference", "parentRef", pRef)
					updated = true
					stop = false
				}
				continue
			default:
				log.Error(logger, err, "Failed to get supported gateway for ParentReference", "parentRef", pRef)
				return false, stop, fmt.Errorf("failed to get supported gateway for parentRef %s: %w", pRef.Name, err)
			}
		}
		if !found {
			continue
		}

		log.Debug(logger, "Building Accepted condition", "parentRef", pRef, "gateway", gateway.Name)
		acceptedCondition, err := route.BuildAcceptedCondition(ctx, logger, c.Client, gateway, c.route, pRef)
		if err != nil {
			return false, stop, fmt.Errorf("failed to build accepted condition for parentRef %s: %w", pRef.Name, err)
		}
		// If the Accepted or ResolvedRefs condition is False, we should stop further processing.
		if acceptedCondition.Status == metav1.ConditionFalse || resolvedRefsCond.Status == metav1.ConditionFalse {
			stop = true
		}

		log.Debug(logger, "Building Programmed conditions", "parentRef", pRef, "gateway", gateway.Name)
		programmedConditions, err := route.BuildProgrammedCondition(ctx, logger, c.Client, c.route, pRef, c.expectedGVKs)
		if err != nil {
			return false, stop, fmt.Errorf("failed to build programmed condition for parentRef %s: %w", pRef.Name, err)
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
		log.Debug(logger, "Updating TLSRoute status in cluster", "status", c.route.Status)
		if err := c.Status().Update(ctx, c.route); err != nil {
			if apierrors.IsConflict(err) {
				return false, true, err
			}
			log.Error(logger, err, "Failed to update TLSRoute status in cluster")
			return false, stop, fmt.Errorf("failed to update TLSRoute status: %w", err)
		}
	} else {
		log.Debug(logger, "No status update required for TLSRoute")
	}

	log.Debug(logger, "Finished UpdateRootObjectStatus", "updated", updated)
	return updated, stop, nil
}

func (c *tlsRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
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

func (c *tlsRouteConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error) {
	am := metadata.NewAnnotationManager(logger)

	// If the route is not present in the hybrid-routes annotation of the Kong resource, don't touch it.
	if !am.ContainsRoute(resource, c.route) {
		log.Trace(logger, "Route annotation not found, skipping resource", "kind", resource.GetKind(), "obj", client.ObjectKeyFromObject(resource))
		return true, nil
	}

	oldResource := resource.DeepCopy()
	am.RemoveRouteFromAnnotation(resource, c.route)

	// If other Routes are still present in the annotation, we just need to update the resource.
	if len(am.GetRoutes(resource)) > 0 {
		log.Debug(logger, "Updating hybrid-routes annotation", "kind", resource.GetKind(), "obj", client.ObjectKeyFromObject(resource))
		if err := c.Patch(ctx, resource, client.MergeFrom(oldResource)); err != nil && !apierrors.IsNotFound(err) {
			return true, fmt.Errorf("failed to update resource: %w", err)
		}
		return true, nil
	}

	// No other routes in the annotation, don't skip deletion.
	return false, nil
}

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

	supportedParentRefs, err := getHybridGatewayParents(ctx, logger, c.Client, c.route, c.route.Spec.ParentRefs)
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
			log.Debug(logger, "Successfully translated KongUpstream resource",
				"upstream", upstreamName)

			// Build the KongService resource.
			servicePtr, err := service.ServiceForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp, upstreamName)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongService resource, skipping rule",
					"controlPlane", cp.KonnectNamespacedRef,
					"upstream", upstreamName)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongService for rule: %w", err))
				continue
			}
			serviceName := servicePtr.Name
			c.outputStore = append(c.outputStore, servicePtr)
			log.Debug(logger, "Successfully translated KongService resource",
				"service", serviceName)

			// Build the KongRoute resource.
			routes, err := kongroute.RoutesForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp, serviceName, hostnames)
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

	}

	return nil
}
