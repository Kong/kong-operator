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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/intermediate"
	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

var _ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	route        *gwtypes.HTTPRoute
	routeStatus  *gwtypes.HTTPRouteStatus
	outputStore  []client.Object
	ir           *intermediate.HTTPRouteRepresentation
	expectedGVKs []schema.GroupVersionKind
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func newHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:      cl,
		outputStore: []client.Object{},
		route:       httpRoute,
		ir:          intermediate.NewHTTPRouteRepresentation(httpRoute),
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

// Reduce implements APIConverter.
func (c *httpRouteConverter) Reduce(obj unstructured.Unstructured) []utils.ReduceFunc {
	switch obj.GetKind() {
	case "KongRoute":
		return []utils.ReduceFunc{
			utils.KeepProgrammed,
			utils.KeepYoungest,
		}
	default:
		return nil
	}
}

// ListExistingObjects implements APIConverter.
func (c *httpRouteConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	if c.route == nil {
		return nil, nil
	}

	list := &configurationv1alpha1.KongRouteList{}
	labels := map[string]string{
		// TODO: Add appropriate labels for KongRoute objects managed by HTTPRoute
	}
	opts := []client.ListOption{
		client.InNamespace(c.route.Namespace),
		client.MatchingLabels(labels),
	}
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	unstructuredItems := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, item := range list.Items {
		unstr, err := utils.ToUnstructured(&item, c.Scheme())
		if err != nil {
			return nil, err
		}
		unstructuredItems = append(unstructuredItems, unstr)
	}

	return unstructuredItems, nil
}

// UpdateSharedRouteStatus implements APIConverter.
func (c *httpRouteConverter) UpdateSharedRouteStatus(objs []unstructured.Unstructured) error {
	// TODO: Implement status update logic for HTTPRoute
	return nil
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
	// For each parentRef in the HTTPRoute, build the conditions and set them in the status.
	logger.V(1).Info("Starting UpdateRootObjectStatus", "route", c.route.Name)
	for _, pRef := range c.route.Spec.ParentRefs {
		logger.V(2).Info("Processing ParentReference", "parentRef", pRef)
		// Check if the parentRef belongs to a Gateway managed by us.
		gateway, err := route.GetSupportedGatewayForParentRef(ctx, logger, c.Client, pRef, c.route.Namespace)
		if err != nil {
			if errors.Is(err, route.ErrNoGatewayClassFound) ||
				errors.Is(err, route.ErrNoGatewayController) ||
				errors.Is(err, route.ErrNoGatewayFound) {
				// If the gateway is not managed by us, not found we skip setting conditions.
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
			return false, fmt.Errorf("failed to update HTTPRoute status for parentRef %s: %w", pRef.Name, err)
		}

		logger.V(2).Info("Building Programmed conditions", "parentRef", pRef, "gateway", gateway.Name)
		programmedConditions, err := route.BuildProgrammedCondition(ctx, logger, c.Client, c.route, pRef, c.expectedGVKs)
		if err != nil {
			return false, fmt.Errorf("failed to build programmed condition for parentRef %s: %w", pRef.Name, err)
		}

		// Combine all conditions.
		programmedConditions = append(programmedConditions, *acceptedCondition)

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
	// Generate translation data.
	if err := c.addControlPlaneRefs(ctx); err != nil {
		return err
	}
	if err := c.addHostnames(ctx); err != nil {
		return err
	}

	// Generate kong services, upstream and targets.
	for _, val := range c.ir.Rules {
		// Get the controlPlaneRef for the given Rule.
		cpr := c.ir.GetControlPlaneRefByName(val.Name)
		if cpr == nil {
			continue
		}
		hostnames := c.ir.GetHostnamesByName(val.Name)
		if hostnames == nil {
			continue
		}
		name := val.String()

		// Build the upstream resource.
		upstream, err := builder.NewKongUpstream().
			WithName(name).
			WithNamespace(c.route.Namespace).
			WithLabels(c.route, c.ir.GetParentRefByName(val.Name)).
			WithAnnotations(c.route, c.ir.GetParentRefByName(val.Name)).
			WithSpecName(name).
			WithControlPlaneRef(*cpr).
			WithOwner(c.route).Build()
		if err != nil {
			// TODO: decide how to handle build errors in converter
			// For now, skip this resource
			continue
		}
		c.outputStore = append(c.outputStore, &upstream)

		// Build the service resource.
		service, err := builder.NewKongService().
			WithName(name).
			WithNamespace(c.route.Namespace).
			WithLabels(c.route, c.ir.GetParentRefByName(val.Name)).
			WithAnnotations(c.route, c.ir.GetParentRefByName(val.Name)).
			WithSpecName(name).
			WithSpecHost(name).
			WithControlPlaneRef(*cpr).
			WithOwner(c.route).Build()
		if err != nil {
			// TODO: decide how to handle build errors in converter
			// For now, skip this resource
			continue
		}
		c.outputStore = append(c.outputStore, &service)

		// Build the target resources.
		for _, bRef := range val.BackendRefs {
			targetName := bRef.String()

			target, err := builder.NewKongTarget().
				WithName(targetName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, c.ir.GetParentRefByName(bRef.Name)).
				WithAnnotations(c.route, c.ir.GetParentRefByName(bRef.Name)).
				WithUpstreamRef(name).
				WithBackendRef(c.route, &bRef.BackendRef).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &target)
		}

		// Build the kong route resource.
		for _, match := range val.Matches {
			routeName := match.String()
			serviceName := val.String()

			route, err := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, c.ir.GetParentRefByName(match.Name)).
				WithAnnotations(c.route, c.ir.GetParentRefByName(match.Name)).
				WithSpecName(routeName).
				WithHosts(hostnames.Hostnames).
				WithStripPath(c.ir.StripPath).
				WithKongService(serviceName).
				WithHTTPRouteMatch(match.Match).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &route)
		}

		// Build the kong plugin and kong plugin binding resources.
		for _, filter := range val.Filters {
			pluginName := filter.String()

			plugin, err := builder.NewKongPlugin().
				WithName(pluginName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route, c.ir.GetParentRefByName(val.Name)).
				WithAnnotations(c.route, c.ir.GetParentRefByName(val.Name)).
				WithFilter(filter.Filter).
				WithOwner(c.route).Build()
			if err != nil {
				continue
			}
			c.outputStore = append(c.outputStore, &plugin)

			// Create a KongPluginBinding to bind the KongPlugin to each rule match.
			for _, match := range val.Matches {
				routeName := match.String()
				bbuild := builder.NewKongPluginBinding().
					WithName(routeName+fmt.Sprintf(".%d", filter.Name.GetFilterIndex())).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route, c.ir.GetParentRefByName(match.Name)).
					WithAnnotations(c.route, c.ir.GetParentRefByName(match.Name)).
					WithPluginRef(pluginName).
					WithControlPlaneRef(*cpr).
					WithOwner(c.route)
				if filter.Filter.Type == gatewayv1.HTTPRouteFilterResponseHeaderModifier {
					// For response header modifiers, bind the plugin to the KongService instead of KongRoute.
					serviceName := val.String()
					bbuild = bbuild.WithServiceRef(serviceName)
				} else {
					bbuild = bbuild.WithRouteRef(routeName)
				}
				binding, err := bbuild.Build()
				if err != nil {
					continue
				}
				c.outputStore = append(c.outputStore, &binding)
			}
		}
	}

	return nil
}

func (c *httpRouteConverter) addControlPlaneRefs(ctx context.Context) error {
	for i, pRef := range c.route.Spec.ParentRefs {
		pRefName := intermediate.NameFromHTTPRoute(c.route, "", i)
		cpRef, err := refs.GetControlPlaneRefByParentRef(ctx, c.Client, c.route, pRef)
		if err != nil {
			return err
		}
		c.ir.AddControlPlaneRef(intermediate.ControlPlaneRef{
			Name:            pRefName,
			ControlPlaneRef: cpRef,
		})
	}
	return nil
}

// addHostnames adds hostnames to the intermediate representation based on the HTTPRoute's ParentRefs
// and their associated Gateways and Listeners. If there is no intersection between the HTTPRoute's hostnames
// and the Listener's hostname, no Hostnames entry is added. If all hostnames are accepted, an entry with an
// empty hostname list is added.
func (c *httpRouteConverter) addHostnames(ctx context.Context) error {

	for i, pRef := range c.route.Spec.ParentRefs {
		var err error
		var listeners []gwtypes.Listener
		hosts := []string{}
		hostnamesName := intermediate.NameFromHTTPRoute(c.route, "", i)
		if listeners, err = refs.GetListenersByParentRef(ctx, c.Client, c.route, pRef); err != nil {
			return err
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
				for _, hostname := range c.route.Spec.Hostnames {
					hosts = append(hosts, string(hostname))
				}
				c.ir.AddHostnames(intermediate.Hostnames{
					Name:      hostnamesName,
					Hostnames: hosts,
				})
				return nil
			}

			// Handle wildcard hostnames - get intersection
			for _, hostname := range c.route.Spec.Hostnames {
				routeHostname := string(hostname)
				if intersection := utils.HostnameIntersection(string(*listener.Hostname), routeHostname); intersection != "" {
					hosts = append(hosts, intersection)
				}
			}
		}
		if len(hosts) > 0 {
			c.ir.AddHostnames(intermediate.Hostnames{
				Name:      hostnamesName,
				Hostnames: hosts,
			})
		}
	}
	return nil
}
