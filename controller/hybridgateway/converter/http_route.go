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
func (c *httpRouteConverter) GetRootObject() gwtypes.HTTPRoute {
	return *c.route
}

// Translate implements APIConverter.
func (c *httpRouteConverter) Translate() error {
	return c.translate(context.TODO())
}

// GetOutputStore implements APIConverter.
func (c *httpRouteConverter) GetOutputStore(ctx context.Context) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(c.outputStore))
	for _, obj := range c.outputStore {
		unstr, err := utils.ToUnstructured(obj, c.Scheme())
		if err != nil {
			continue
		}
		objects = append(objects, unstr)
	}
	return objects
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
	updated := false

	// First, build the resolvedRefs conditons for the HTTPRoute since it is the same for all ParentRefs.
	logger.V(1).Info("Building ResolvedRefs condition for HTTPRoute", "route", c.route.Name)
	resolvedRefsCond, err := route.BuildResolvedRefsCondition(ctx, logger, c.Client, c.route, c.referenceGrantEnabled)
	if err != nil {
		return false, fmt.Errorf("failed to build resolvedRefs condition for HTTPRoute %s: %w", c.route.Name, err)
	}

	// For each parentRef in the HTTPRoute, build the conditions and set them in the status.
	logger.V(1).Info("Starting UpdateRootObjectStatus", "route", c.route.Name)
	for _, pRef := range c.route.Spec.ParentRefs {
		logger.V(2).Info("Processing ParentReference", "parentRef", pRef)
		// Check if the parentRef belongs to a Gateway managed by us.
		gateway, err := route.GetSupportedGatewayForParentRef(ctx, logger, c.Client, pRef, c.route.Namespace)
		if err != nil {
			if errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound) ||
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController) ||
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound) {
				// If the gateway is not managed by us or not found we skip setting conditions.
				logger.V(1).Info("Skipping status update for unsupported or non-existent Gateway", "parentRef", pRef)
				if route.RemoveStatusForParentRef(logger, c.route, pRef, vars.ControllerName()) {
					// If we removed the status, we need to mark the update as true.
					logger.V(2).Info("Removed ParentStatus for unsupported ParentReference", "parentRef", pRef)
					updated = true
				}
				continue
			} else {
				logger.Error(err, "Failed to get supported gateway for ParentReference", "parentRef", pRef)
				return false, fmt.Errorf("failed to get supported gateway for parentRef %s: %w", pRef.Name, err)
			}
		}

		logger.V(2).Info("Building Accepted condition", "parentRef", pRef, "gateway", gateway.Name)
		acceptedCondition, err := route.BuildAcceptedCondition(ctx, logger, c.Client, gateway, c.route, pRef)
		if err != nil {
			return false, fmt.Errorf("failed to build accepted condition for parentRef %s: %w", pRef.Name, err)
		}

		logger.V(2).Info("Building Programmed conditions", "parentRef", pRef, "gateway", gateway.Name)
		programmedConditions, err := route.BuildProgrammedCondition(ctx, logger, c.Client, c.route, pRef, c.expectedGVKs)
		if err != nil {
			return false, fmt.Errorf("failed to build programmed condition for parentRef %s: %w", pRef.Name, err)
		}

		// Combine all conditions.
		programmedConditions = append(programmedConditions, *acceptedCondition, *resolvedRefsCond)

		logger.V(2).Info("Setting status conditions", "parentRef", pRef, "conditionsCount", len(programmedConditions))
		if route.SetStatusConditions(c.route, pRef, vars.ControllerName(), programmedConditions...) {
			logger.V(1).Info("Status conditions updated for ParentReference", "parentRef", pRef)
			updated = true
		}
	}

	logger.V(2).Info("Cleaning up orphaned ParentStatus entries", "route", c.route.Name)
	if route.CleanupOrphanedParentStatus(logger, c.route, vars.ControllerName()) {
		logger.V(1).Info("Orphaned ParentStatus entries cleaned up", "route", c.route.Name)
		updated = true
	}

	// Update the status in the cluster if there are changes.
	if updated {
		logger.V(1).Info("Updating HTTPRoute status in cluster", "route", c.route.Name, "status", c.route.Status)
		if err := c.Status().Update(ctx, c.route); err != nil {
			logger.Error(err, "Failed to update HTTPRoute status in cluster", "route", c.route.Name)
			return false, fmt.Errorf("failed to update HTTPRoute status: %w", err)
		}
	} else {
		logger.V(1).Info("No status update required for HTTPRoute", "route", c.route.Name)
	}

	logger.V(1).Info("Finished UpdateRootObjectStatus", "route", c.route.Name, "updated", updated)
	return updated, nil
}

// translate converts the HTTPRoute to KongRoute(s) and stores them in outputStore.
func (c *httpRouteConverter) translate(ctx context.Context) error {
	supportedParentRefs, err := c.getHybridGatewayParents(ctx)
	if err != nil {
		return err
	}
	if len(supportedParentRefs) == 0 {
		return nil
	}

	httpRouteName := c.route.Namespace + "-" + c.route.Name

	for _, pRefData := range supportedParentRefs {
		pRef := pRefData.parentRef
		cp := pRefData.cpRef
		hostnames := pRefData.hostnames
		cpRefName := "cp" + utils.Hash32(cp)

		for _, rule := range c.route.Spec.Rules {
			// Build the KongUpstream resource.
			upstreamName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.BackendRefs)).String()
			upstream, err := builder.NewKongUpstream().
				WithName(upstreamName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, &pRef).
				WithAnnotations(c.route, &pRef).
				WithSpecName(upstreamName).
				WithControlPlaneRef(*cp).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &upstream)

			// Build the KongTarget resources using the new rule-based approach.
			targets, err := target.TargetsForBackendRefs(
				ctx,
				logr.Discard(), // TODO: pass proper logger.
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
				// TODO: decide how to handle target creation errors in converter
				// For now, skip this rule
				continue
			}
			for _, tgt := range targets {
				c.outputStore = append(c.outputStore, &tgt)
			}

			// Build the KongService resource.
			serviceName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.BackendRefs)).String()
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
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &service)

			// Build the kong route resource.
			routeName := namegen.NewName(httpRouteName, cpRefName, utils.Hash32(rule.Matches)).String()
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
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &route)

			// Build the kong plugin and kong plugin binding resources.
			for _, filter := range rule.Filters {
				filterHash := utils.Hash32(filter)
				pluginName := namegen.NewName(httpRouteName, cpRefName, filterHash).String()
				plugin, err := builder.NewKongPlugin().
					WithName(pluginName).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route, &pRef).
					WithAnnotations(c.route, &pRef).
					WithFilter(filter).
					WithOwner(c.route).Build()
				if err != nil {
					continue
				}
				c.outputStore = append(c.outputStore, &plugin)

				// Create a KongPluginBinding to bind the KongPlugin to each rule match.
				binding, err := builder.NewKongPluginBinding().
					WithName(routeName+"."+filterHash).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route, &pRef).
					WithAnnotations(c.route, &pRef).
					WithPluginRef(pluginName).
					WithControlPlaneRef(*cp).
					WithOwner(c.route).
					WithRouteRef(routeName).Build()
				if err != nil {
					continue
				}
				c.outputStore = append(c.outputStore, &binding)
			}
		}
	}

	return nil
}

type hybridGatewayParent struct {
	parentRef gwtypes.ParentReference
	cpRef     *commonv1alpha1.ControlPlaneRef
	hostnames []string
}

func (c *httpRouteConverter) getHybridGatewayParents(ctx context.Context) ([]hybridGatewayParent, error) {
	result := []hybridGatewayParent{}

	for _, pRef := range c.route.Spec.ParentRefs {
		cp, err := c.getControlPlaneRefByParentRef(ctx, pRef)
		if err != nil {
			return nil, err
		}
		if cp == nil {
			continue
		}

		hostnames, err := c.getHostnamesByParentRef(ctx, pRef)
		if err != nil {
			return nil, err
		}
		if hostnames == nil {
			continue
		}

		result = append(result, hybridGatewayParent{
			parentRef: pRef,
			cpRef:     cp,
			hostnames: hostnames,
		})
	}

	return result, nil
}

func (c *httpRouteConverter) getControlPlaneRefByParentRef(ctx context.Context, pRef gwtypes.ParentReference) (*commonv1alpha1.ControlPlaneRef, error) {
	return refs.GetControlPlaneRefByParentRef(ctx, c.Client, c.route, pRef)
}

func (c *httpRouteConverter) getHostnamesByParentRef(ctx context.Context, pRef gwtypes.ParentReference) ([]string, error) {
	var err error
	var hostnames []string

	listeners, err := refs.GetListenersByParentRef(ctx, c.Client, c.route, pRef)
	if err != nil {
		return nil, err
	}

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
			hostnames = []string{}
			for _, host := range c.route.Spec.Hostnames {
				hostnames = append(hostnames, string(host))
			}
			return hostnames, nil
		}

		// Handle wildcard hostnames - get intersection
		for _, host := range c.route.Spec.Hostnames {
			routeHostname := string(host)
			if intersection := utils.HostnameIntersection(string(*listener.Hostname), routeHostname); intersection != "" {
				hostnames = append(hostnames, intersection)
			}
		}
	}
	return hostnames, nil
}
