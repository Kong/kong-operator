package converter

import (
	"context"
	"strings"

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
)

var _ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	route           *gwtypes.HTTPRoute
	outputStore     []client.Object
	sharedStatusMap *route.SharedRouteStatusMap
	ir              *intermediate.HTTPRouteRepresentation
	expectedGVKs    []schema.GroupVersionKind
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func newHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client, sharedStatusMap *route.SharedRouteStatusMap) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:          cl,
		outputStore:     []client.Object{},
		sharedStatusMap: sharedStatusMap,
		route:           httpRoute,
		ir:              intermediate.NewHTTPRouteRepresentation(httpRoute),
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

func (c *httpRouteConverter) translate(ctx context.Context) error {
	// TODO: consider dropping the intermediate representation and doing everything in one pass.
	// ControlPlaneRefs and Hostnames should be calculated at each pass in the parentRefs loop.
	if err := c.addControlPlaneRefs(ctx); err != nil {
		return err
	}
	if err := c.addHostnames(ctx); err != nil {
		return err
	}

	// TODO: Should we hash the httproute namespace/name to ensure length limits?
	// or may we drop it?
	httpRouteName := strings.Join([]string{c.route.Namespace, c.route.Name}, "-")
	for i, pRef := range c.route.Spec.ParentRefs {
		pRefName := intermediate.NameFromHTTPRoute(c.route, "", i)
		cpRef, err := refs.GetControlPlaneRefByParentRef(ctx, c.Client, c.route, pRef)
		if err != nil {
			return err
		}
		if cpRef == nil {
			continue
		}
		hostnames := c.ir.GetHostnamesByName(pRefName)
		if hostnames == nil {
			continue
		}

		//pRefHash := utils.Hash32(pRef)
		cpRefName := cpRef.KonnectNamespacedRef.Name

		// For each rule in the HTTPRoute, create intermediate representation entries.
		// TODO: move each resource creation to its own function to improve readability.
		for _, rule := range c.route.Spec.Rules {
			// Build the KongUpstream resource.
			// name - scope: pRef/cpRef , id from BackendRefs
			// TODO: httpRouteName can be dropped
			upstreamName := strings.Join([]string{httpRouteName, cpRefName, utils.Hash32(rule.BackendRefs)}, ".")
			upstream, err := builder.NewKongUpstream().
				WithName(upstreamName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
				WithAnnotations(c.route, &pRef).
				WithSpecName(upstreamName).
				WithControlPlaneRef(*cpRef).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &upstream)

			// Build the KongTarget resources.
			for _, bRef := range rule.BackendRefs {
				// Since KongTarget has a reference to a KongUpstream which changes if any of the backends change,
				// we need to identify the KongTarget by hashing the entire BackendRefs slice. To have it unique
				// we include the index.
				// name - scope: pRef/cpRef, id from single BackendRef
				// TODO: httpRouteName can be dropped
				targetName := strings.Join([]string{httpRouteName, cpRefName, utils.Hash32(bRef)}, ".")
				target, err := builder.NewKongTarget().
					WithName(targetName).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route).
					WithAnnotations(c.route, &pRef).
					WithUpstreamRef(upstreamName).
					WithBackendRef(c.route, &bRef).
					WithOwner(c.route).Build()
				if err != nil {
					// TODO: decide how to handle build errors in converter
					// For now, skip this resource
					continue
				}
				c.outputStore = append(c.outputStore, &target)
			}

			// Build the KongService resource.
			// name - scope: pRef/cpRef , id from Matches
			// TODO: httpRouteName can be dropped - same match on same cpRef in different httpRoutes makes no sense, right?
			serviceName := strings.Join([]string{httpRouteName, cpRefName, utils.Hash32(rule.Matches)}, ".")
			service, err := builder.NewKongService().
				WithName(serviceName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
				WithAnnotations(c.route, &pRef).
				WithSpecName(serviceName).
				WithSpecHost(upstreamName).
				WithControlPlaneRef(*cpRef).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &service)

			// Build the KongRoute resources.
			routeName := serviceName
			rbuild := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
				WithAnnotations(c.route, &pRef).
				WithSpecName(routeName).
				WithHosts(hostnames.Hostnames).
				WithStripPath(c.ir.StripPath).
				WithKongService(serviceName).
				WithOwner(c.route)
			for _, match := range rule.Matches {
				rbuild = rbuild.WithHTTPRouteMatch(match)
			}
			route, err := rbuild.Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &route)

			// Build the KongPlugin and KongPluginBinding resources.
			for _, filter := range rule.Filters {
				// KongPlugins are identified by the plugin itself. Multiple KongPluginBindings
				// can point to the same KongPlugin (is this true?).
				pluginName := strings.Join([]string{httpRouteName, cpRefName, utils.Hash32(filter)}, ".")

				plugin, err := builder.NewKongPlugin().
					WithName(pluginName).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route).
					WithAnnotations(c.route, &pRef).
					WithFilter(filter).
					WithOwner(c.route).Build()
				if err != nil {
					continue
				}
				c.outputStore = append(c.outputStore, &plugin)

				// Create a KongPluginBinding to bind the KongPlugin to each rule match.
				var bindingName string
				bbuild := builder.NewKongPluginBinding().
					WithNamespace(c.route.Namespace).
					WithLabels(c.route).
					WithAnnotations(c.route, &pRef).
					WithPluginRef(pluginName).
					WithControlPlaneRef(*cpRef).
					WithOwner(c.route)
				if filter.Type == gatewayv1.HTTPRouteFilterResponseHeaderModifier {
					// For response header modifiers, bind the plugin to the KongService instead of KongRoute.
					bindingName = strings.Join([]string{serviceName, "plugin", utils.Hash32(filter)}, ".")
					bbuild = bbuild.WithServiceRef(serviceName)
				} else {
					bindingName = strings.Join([]string{routeName, "plugin", utils.Hash32(filter)}, ".")
					bbuild = bbuild.WithRouteRef(routeName)
				}
				binding, err := bbuild.WithName(bindingName).Build()
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
				if intersection := hostnameIntersection(string(*listener.Hostname), routeHostname); intersection != "" {
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

// hostnameIntersection returns the intersection of listener and route hostnames.
// Returns the most specific hostname that satisfies both constraints, or an empty string if
// there is no intersection.
func hostnameIntersection(listenerHostname, routeHostname string) string {
	// Exact match - return the common hostname
	if listenerHostname == routeHostname {
		return routeHostname
	}

	// Listener is wildcard (*.example.com), route is specific (api.example.com)
	if strings.HasPrefix(listenerHostname, "*.") {
		wildcardDomain := listenerHostname[1:] // Remove "*"

		// Route hostname must end with the wildcard domain
		if strings.HasSuffix(routeHostname, wildcardDomain) {
			return routeHostname // Return the more specific route hostname
		}
	}

	// Route is wildcard (*.example.com), listener is specific (api.example.com)
	if strings.HasPrefix(routeHostname, "*.") {
		wildcardDomain := routeHostname[1:] // Remove "*"

		// Listener hostname must end with the wildcard domain
		if strings.HasSuffix(listenerHostname, wildcardDomain) {
			return listenerHostname // Return the more specific listener hostname
		}
	}

	return "" // No intersection
}
