package converter

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

type BackendRefTarget struct {
	Name   string
	Host   string
	Port   gwtypes.PortNumber
	Weight *int32
}

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
			WithLabels(c.route).
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
			WithLabels(c.route).
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
			targets, err := c.getTargets(ctx, bRef)
			if err != nil {
				// If we can't get targets for a backend ref, skip it.
				continue
			}
			for _, target := range targets {
				kongTarget, err := builder.NewKongTarget().
					WithName(target.Name).
					WithNamespace(c.route.Namespace).
					WithLabels(c.route).
					WithAnnotations(c.route, c.ir.GetParentRefByName(bRef.Name)).
					WithUpstreamRef(name).
					WithTarget(target.Host, target.Port).
					WithWeight(target.Weight).
					WithOwner(c.route).Build()
				if err != nil {
					// TODO: decide how to handle build errors in converter
					// For now, skip this resource
					continue
				}
				c.outputStore = append(c.outputStore, &kongTarget)
			}
		}

		// Build the kong route resource.
		for _, match := range val.Matches {
			routeName := match.String()
			serviceName := val.String()

			route, err := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
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
				WithLabels(c.route).
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
					WithLabels(c.route).
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

// getTargets get targets to the intermediate representation based on the BackendRefs in the HTTPRoute.
func (c *httpRouteConverter) getTargets(ctx context.Context, bRef intermediate.BackendRef) ([]BackendRefTarget, error) {
	targets := []BackendRefTarget{}

	// retrieve the service with name and namespace from the BackendRef
	svcNamespace := namespaceFromBackendRef(bRef.BackendRef, c.route.Namespace)
	svc := &corev1.Service{}
	err := c.Client.Get(ctx, client.ObjectKey{Name: string(bRef.BackendRef.Name), Namespace: svcNamespace}, svc)
	if err != nil {
		// If the service is not found, return an empty target list (it might be created later).
		return nil, err
	}
	// find the port in the service that matches the port in the BackendRef
	svcPort, svcPortFound := lo.Find(svc.Spec.Ports, func(p corev1.ServicePort) bool {
		return p.Port == int32(*bRef.BackendRef.Port)
	})
	if !svcPortFound {
		// If the port is not found, return an empty target list (it might be created later).
		return nil, fmt.Errorf("port %v not found in service %s/%s", *bRef.BackendRef.Port, svcNamespace, svc.Name)
	}

	// TODO: we have to add a way to configure if we want to use EndpointSlices or Service FQDN
	// For now, we will always use EndpointSlices if available
	// If you want to use Service FQDN, uncomment the following block
	if false {
		// Use Service FQDN as target
		target := BackendRefTarget{
			Name:   bRef.Name.String(),
			Host:   TargetHostAsServiceFQDN(bRef.BackendRef, c.route.Namespace),
			Port:   *bRef.BackendRef.Port,
			Weight: bRef.BackendRef.Weight,
		}
		targets = append(targets, target)

		return targets, nil
	}

	// Use EndpointSlices as targets
	// List EndpointSlices for the service
	// Reference: https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/
	// Note: EndpointSlices are namespaced resources
	// Reference: https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#endpointslice-v1-discovery-k8s-io
	endpointSlices := &discoveryv1.EndpointSliceList{}
	req, err := labels.NewRequirement(discoveryv1.LabelServiceName, selection.Equals, []string{svc.Name})
	if err != nil {
		return nil, err
	}
	labelSelector := labels.NewSelector().Add(*req)
	err = c.Client.List(ctx, endpointSlices, &client.ListOptions{Namespace: svcNamespace, LabelSelector: labelSelector})

	if err == nil {
		for _, endpointSlice := range endpointSlices.Items {
			leng := len(endpointSlice.Endpoints)

			for _, p := range endpointSlice.Ports {
				if p.Port == nil || *p.Port < 0 || *p.Protocol != svcPort.Protocol || *p.Name != svcPort.Name {
					continue
				}
				upstreamPort := *p.Port

				for _, endpoint := range endpointSlice.Endpoints {
					if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
						// Skip not ready endpoints
						continue
					}

					for _, addr := range endpoint.Addresses {
						weight := *bRef.BackendRef.Weight / int32(leng)
						target := BackendRefTarget{
							Name:   fmt.Sprintf("%s-%s", bRef.Name.String(), strings.ReplaceAll(addr, ".", "-")),
							Host:   addr,
							Port:   gwtypes.PortNumber(upstreamPort),
							Weight: &weight,
						}
						targets = append(targets, target)
					}
				}
			}
		}
	}
	return targets, nil
}

// TargetHostAsServiceFQDN constructs the fully qualified domain name (FQDN) for a backend service.
// It combines the backend reference name, namespace, and standard Kubernetes service domain suffix.
func TargetHostAsServiceFQDN(bRef gwtypes.HTTPBackendRef, defaultNamespace string) string {
	namespace := namespaceFromBackendRef(bRef, defaultNamespace)
	return string(bRef.Name) + "." + namespace + ".svc.cluster.local"
}

// namespaceFromBackendRef extracts the namespace from a BackendRef.
// If the BackendRef does not specify a namespace, it defaults to the provided defaultNamespace.
func namespaceFromBackendRef(bRef gwtypes.HTTPBackendRef, defaultNamespace string) string {
	if bRef.Namespace != nil {
		return string(*bRef.Namespace)
	}
	return defaultNamespace
}
