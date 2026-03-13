package converter

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/kongroute"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/service"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/target"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/upstream"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

var _ APIConverter[gwtypes.TLSRoute] = &tlsRouteConverter{}

type tlsRouteConverter struct {
	client.Client

	route        *gwtypes.TLSRoute
	outputStore  []client.Object
	expectedGVKs []schema.GroupVersionKind
}

func newTLSRouteConverter(tlsRoute *gwtypes.TLSRoute, cl client.Client) APIConverter[gwtypes.TLSRoute] {
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
	}
}

func (c *tlsRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

func (c *tlsRouteConverter) GetRootObject() gwtypes.TLSRoute {
	return *c.route
}

func (c *tlsRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	return false, false, nil
}

func (c *tlsRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	return nil, nil
}

func (c *tlsRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	if err := c.translate(ctx, logger); err != nil {
		return 0, err
	}
	return len(c.outputStore), nil
}

func (c *tlsRouteConverter) translate(ctx context.Context, logger logr.Logger) error {

	logger = logger.WithValues("phase", "tlsroute-translate")
	log.Debug(logger, "Starting HTTPRoute translation")

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
			routePtr, err := kongroute.RouteForRule(ctx, logger, c.Client, c.route, rule, &pRef, cp, serviceName, hostnames)
			if err != nil {
				log.Error(logger, err, "Failed to translate KongRoute resource, skipping rule",
					"service", serviceName,
					"hostnames", hostnames)
				translationErrors = append(translationErrors, fmt.Errorf("failed to translate KongRoute for rule: %w", err))
				continue
			}
			routeName := routePtr.Name
			c.outputStore = append(c.outputStore, routePtr)
			log.Debug(logger, "Successfully translated KongRoute resource",
				"route", routeName)

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
