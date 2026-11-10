package translator

import (
	"maps"
	"slices"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/logging"
	mgrconsts "github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

// Kinds of the Gateway API routes whose translated Kong entities inherit tags from their parent
// Gateways and those Gateways' GatewayClasses.
const (
	kindHTTPRoute = "HTTPRoute"
	kindGRPCRoute = "GRPCRoute"
	kindTCPRoute  = "TCPRoute"
	kindUDPRoute  = "UDPRoute"
	kindTLSRoute  = "TLSRoute"
)

// inheritedTags holds the konghq.com/tags an entity inherits, keeping the Gateway tags apart from
// the GatewayClass tags so that an aggregation over several sources can emit every Gateway tag
// before any GatewayClass tag.
type inheritedTags struct {
	gateway      []string
	gatewayClass []string
}

// all returns the Gateway tags followed by the GatewayClass tags, deduplicated.
func (it inheritedTags) all() []string {
	return lo.FromSlicePtr(util.MergeTags(nil, it.gateway, it.gatewayClass))
}

func (it inheritedTags) isEmpty() bool {
	return len(it.gateway) == 0 && len(it.gatewayClass) == 0
}

// inheritedTagIndex maps the key of a Gateway API route (see routeKey) to the tags its translated
// Kong entities inherit. Routes that inherit no tags are not present.
type inheritedTagIndex map[string]inheritedTags

func routeKey(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

// routeKeyForObjectInfo returns the key of the Gateway API route a Kong route was translated from.
func routeKeyForObjectInfo(info util.K8sObjectInfo) (string, bool) {
	if info.GroupVersionKind.Group != gatewayapi.GroupVersion.Group {
		return "", false
	}
	switch kind := info.GroupVersionKind.Kind; kind {
	case kindHTTPRoute, kindGRPCRoute, kindTCPRoute, kindUDPRoute, kindTLSRoute:
		return routeKey(kind, info.Namespace, info.Name), true
	default:
		return "", false
	}
}

// routeKeyForObject returns the key of obj when it is a Gateway API route. The kind comes from the
// concrete Go type rather than from TypeMeta, which is not guaranteed to be populated.
func routeKeyForObject(obj client.Object) (string, bool) {
	var kind string
	switch obj.(type) {
	case *gatewayapi.HTTPRoute:
		kind = kindHTTPRoute
	case *gatewayapi.GRPCRoute:
		kind = kindGRPCRoute
	case *gatewayapi.TCPRoute:
		kind = kindTCPRoute
	case *gatewayapi.UDPRoute:
		kind = kindUDPRoute
	case *gatewayapi.TLSRoute:
		kind = kindTLSRoute
	default:
		return "", false
	}
	return routeKey(kind, obj.GetNamespace(), obj.GetName()), true
}

// inheritedTagResolver resolves the konghq.com/tags inherited from Gateways and their
// GatewayClasses, memoizing them per Gateway for the duration of one translation.
//
// It never fails: a parentRef that does not name a Gateway, a Gateway that is not in the store
// (i.e. not one this controller reconciles) and a GatewayClass that cannot be read or is not
// controlled by this controller all simply contribute no tags. Tag inheritance must never break
// translation.
type inheritedTagResolver struct {
	logger   logr.Logger
	storer   store.Storer
	gateways map[k8stypes.NamespacedName]inheritedTags
}

func newInheritedTagResolver(logger logr.Logger, storer store.Storer) *inheritedTagResolver {
	return &inheritedTagResolver{
		logger:   logger,
		storer:   storer,
		gateways: map[k8stypes.NamespacedName]inheritedTags{},
	}
}

// forGateway returns the tags of gw followed by the tags of its GatewayClass.
func (r *inheritedTagResolver) forGateway(gw *gatewayapi.Gateway) inheritedTags {
	nn := k8stypes.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}
	if tags, ok := r.gateways[nn]; ok {
		return tags
	}

	tags := inheritedTags{
		gateway: annotations.ExtractUserTags(gw.Annotations),
	}
	gwc, err := r.storer.GetGatewayClass(string(gw.Spec.GatewayClassName))
	switch {
	case err != nil:
		r.logger.V(logging.DebugLevel).Info("Failed to get GatewayClass for tag inheritance",
			"gateway", nn, "gatewayclass", gw.Spec.GatewayClassName, "error", err)
	case !gatewayapi.GatewayClassControlledBy(gwc, mgrconsts.GetControllerName()):
		// Not ours, its tags must not leak onto our entities.
	default:
		tags.gatewayClass = annotations.ExtractUserTags(gwc.Annotations)
	}

	r.gateways[nn] = tags
	return tags
}

// forParentRefs returns the tags inherited from every Gateway the parentRefs of a route in
// routeNamespace point to: all the Gateways' tags, followed by all their GatewayClasses' tags.
func (r *inheritedTagResolver) forParentRefs(routeNamespace string, parentRefs []gatewayapi.ParentReference) inheritedTags {
	var (
		tags inheritedTags
		seen = map[k8stypes.NamespacedName]struct{}{}
	)
	for _, pr := range parentRefs {
		// Only Gateways contribute tags.
		if pr.Group != nil && *pr.Group != gatewayapi.V1Group {
			continue
		}
		if pr.Kind != nil && *pr.Kind != KindGateway {
			continue
		}
		nn := parentRefGatewayNN(pr, routeNamespace)
		// A route may reference the same Gateway multiple times (e.g. for different listeners).
		if _, ok := seen[nn]; ok {
			continue
		}
		seen[nn] = struct{}{}

		// The store only holds Gateways reconciled by this controller, so a Gateway owned by another
		// implementation is not found here and contributes no tags.
		gw, err := r.storer.GetGateway(nn.Namespace, nn.Name)
		if err != nil {
			r.logger.V(logging.TraceLevel).Info("Parent Gateway not found for tag inheritance",
				"gateway", nn, "error", err)
			continue
		}
		gwTags := r.forGateway(gw)
		tags.gateway = append(tags.gateway, gwTags.gateway...)
		tags.gatewayClass = append(tags.gatewayClass, gwTags.gatewayClass...)
	}
	return tags
}

// buildInheritedTagIndex resolves, for every Gateway API route in the store, the tags its
// translated Kong entities inherit from its parent Gateways and their GatewayClasses.
func (t *Translator) buildInheritedTagIndex(resolver *inheritedTagResolver) inheritedTagIndex {
	index := inheritedTagIndex{}
	add := func(kind string, route client.Object, parentRefs []gatewayapi.ParentReference) {
		if tags := resolver.forParentRefs(route.GetNamespace(), parentRefs); !tags.isEmpty() {
			index[routeKey(kind, route.GetNamespace(), route.GetName())] = tags
		}
	}
	logListError := func(kind string, err error) {
		t.logger.Error(err, "Failed to list routes for tag inheritance", "kind", kind)
	}

	if routes, err := t.storer.ListHTTPRoutes(); err != nil {
		logListError(kindHTTPRoute, err)
	} else {
		for _, r := range routes {
			add(kindHTTPRoute, r, r.Spec.ParentRefs)
		}
	}
	if routes, err := t.storer.ListGRPCRoutes(); err != nil {
		logListError(kindGRPCRoute, err)
	} else {
		for _, r := range routes {
			add(kindGRPCRoute, r, r.Spec.ParentRefs)
		}
	}
	if routes, err := t.storer.ListTCPRoutes(); err != nil {
		logListError(kindTCPRoute, err)
	} else {
		for _, r := range routes {
			add(kindTCPRoute, r, r.Spec.ParentRefs)
		}
	}
	if routes, err := t.storer.ListUDPRoutes(); err != nil {
		logListError(kindUDPRoute, err)
	} else {
		for _, r := range routes {
			add(kindUDPRoute, r, r.Spec.ParentRefs)
		}
	}
	if routes, err := t.storer.ListTLSRoutes(); err != nil {
		logListError(kindTLSRoute, err)
	} else {
		for _, r := range routes {
			add(kindTLSRoute, r, r.Spec.ParentRefs)
		}
	}

	return index
}

// applyInheritedTags makes every Kong entity translated from a Gateway API route inherit the
// konghq.com/tags of the route's parent Gateways and of those Gateways' GatewayClasses.
//
// Inherited tags are appended after the entity's own tags: first the Gateways', then the
// GatewayClasses'. It must run before populateServices, which computes the Services' (and thereby
// the Upstreams') tags taking kongstate.Service.InheritedTags into account.
//
// It sets:
//   - the tags of every Kong route and of the plugins generated from its filters, and
//     kongstate.Route.InheritedTags (used for KongPlugins bound to the route),
//   - kongstate.Service.InheritedTags: the union over every route the Service was translated from,
//     ordered by route key so that it does not depend on translation order,
//   - the tags of the plugins generated for the Service (e.g. request-termination).
func (t *Translator) applyInheritedTags(ir *ingressRules, index inheritedTagIndex) {
	if len(index) == 0 {
		return
	}

	for name, service := range ir.ServiceNameToServices {
		contributing := map[string]inheritedTags{}
		if service.Parent != nil {
			if key, ok := routeKeyForObject(service.Parent); ok {
				if tags, ok := index[key]; ok {
					contributing[key] = tags
				}
			}
		}

		for i := range service.Routes {
			route := &service.Routes[i]
			key, ok := routeKeyForObjectInfo(route.Ingress)
			if !ok {
				continue
			}
			tags, ok := index[key]
			if !ok {
				continue
			}
			contributing[key] = tags
			applyInheritedTagsToRoute(route, tags.all())
		}

		if len(contributing) == 0 {
			continue
		}

		var aggregated inheritedTags
		for _, key := range slices.Sorted(maps.Keys(contributing)) {
			aggregated.gateway = append(aggregated.gateway, contributing[key].gateway...)
			aggregated.gatewayClass = append(aggregated.gatewayClass, contributing[key].gatewayClass...)
		}
		service.InheritedTags = aggregated.all()
		for i := range service.Plugins {
			service.Plugins[i].Tags = util.MergeTags(service.Plugins[i].Tags, service.InheritedTags)
		}

		ir.ServiceNameToServices[name] = service
	}
}

func applyInheritedTagsToRoute(route *kongstate.Route, tags []string) {
	route.InheritedTags = tags
	// MergeTags always allocates, so this also breaks the aliasing between the route's tags and the
	// tags of the plugins generated from its filters.
	route.Tags = util.MergeTags(route.Tags, tags)
	for i := range route.Plugins {
		route.Plugins[i].Tags = util.MergeTags(route.Plugins[i].Tags, tags)
	}
}
