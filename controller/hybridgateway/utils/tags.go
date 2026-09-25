package utils

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
)

const (
	// MaxTags mirrors the kubebuilder validation:MaxItems=20 marker on commonv1alpha1.Tags.
	// Merged tag sets are capped here because exceeding the limit makes the Server-Side Apply of
	// the Kong resource fail CRD validation, which would take the whole route down.
	MaxTags = 20

	// maxTagLength mirrors the CEL rule "size(tag) >= 1 && size(tag) <= 128" on
	// commonv1alpha1.Tags. It matches the truncation the Konnect ops layer applies in
	// controller/konnect/ops.GenerateTagsForObject.
	maxTagLength = 128
)

// supportedRouteKinds lists the route kinds tracked in the hybrid-routes annotations of a Kong
// resource. It mirrors the kinds handled by metadata.AnnotationManager.RouteAnnotationKeyForKind.
var supportedRouteKinds = []string{"HTTPRoute", "GRPCRoute", "TLSRoute", "TCPRoute", "UDPRoute"}

// TagsFromBackendRefs returns the konghq.com/tags of the first backend Service
// (in backendRefs order) that carries the annotation. Returns nil when no
// referenced Service has the annotation, or none can be fetched. This mirrors
// the first-wins resolution used for other backend-Service annotations
// (e.g. konghq.com/protocol).
func TagsFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) []string {
	for _, ref := range backendRefs {
		if !IsBackendRefSupported(ref.Group, ref.Kind) {
			continue
		}
		ns := namespace
		if ref.Namespace != nil && *ref.Namespace != "" {
			ns = string(*ref.Namespace)
		}
		svc := &corev1.Service{}
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(ref.Name)}, svc); err != nil {
			log.Debug(logger, "Failed to fetch backend Service for tags annotation check",
				"service", fmt.Sprintf("%s/%s", ns, ref.Name), "error", err)
			continue
		}
		if tags := pkgmetadata.ExtractTags(svc); len(tags) > 0 {
			return tags
		}
	}
	return nil
}

// MergeTags returns the ordered union of groups: every tag is whitespace-trimmed, empty tags are
// dropped, over-long tags are truncated to maxTagLength, duplicates are removed keeping the first
// occurrence, and the result is capped at MaxTags.
//
// Order is significant. Callers pass the groups most-specific-first - the entity's own tags, then
// the parent Gateway's, then the GatewayClass's - so that the least specific tags are the ones
// dropped when the merged set is over budget.
//
// Returns nil (never an empty non-nil slice) when nothing survives, which preserves the
// no-op-on-empty contract of the builders' WithSpecTags methods.
func MergeTags(logger logr.Logger, groups ...[]string) []string {
	var (
		merged  []string
		dropped []string
		seen    = map[string]struct{}{}
	)

	for _, group := range groups {
		for _, tag := range group {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if len(tag) > maxTagLength {
				if runes := []rune(tag); len(runes) > maxTagLength {
					tag = string(runes[:maxTagLength])
				}
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			if len(merged) == MaxTags {
				dropped = append(dropped, tag)
				continue
			}
			merged = append(merged, tag)
		}
	}

	if len(dropped) > 0 {
		log.Info(logger, "Dropped tags exceeding the maximum allowed count",
			"max", MaxTags, "kept", merged, "dropped", dropped)
	}

	return merged
}

// InheritedTagsForRoute returns the konghq.com/tags carried by every parent Gateway of route,
// followed by the tags carried by those Gateways' GatewayClasses.
//
// It never fails: a parentRef that does not name a Gateway, a Gateway that cannot be read and a
// missing GatewayClass all simply contribute no tags. Tag inheritance must never break translation.
func InheritedTagsForRoute(ctx context.Context, logger logr.Logger, cl client.Client, route client.Object) []string {
	pair := inheritedTagsForRoute(ctx, logger, cl, route)
	return slices.Concat(pair.gateway, pair.gatewayClass)
}

// InheritedTagsForParentRef returns the konghq.com/tags carried by the Gateway that pRef names,
// followed by the tags of that Gateway's GatewayClass. Use it for entities whose identity is
// scoped to a single parentRef - a KongRoute and its KongPluginBindings - so they do not pick up
// tags from the route's other parents, which need not even be Konnect Gateways.
//
// Like the other resolvers it never fails: an unresolvable Gateway or GatewayClass simply
// contributes no tags.
func InheritedTagsForParentRef(
	ctx context.Context, logger logr.Logger, cl client.Client,
	route client.Object, pRef *gwtypes.ParentReference,
) []string {
	if pRef == nil {
		return nil
	}
	pair := inheritedTagsForParentRef(ctx, logger, cl, route, *pRef)
	return slices.Concat(pair.gateway, pair.gatewayClass)
}

// InheritedTagsForKongObject returns the tags a Kong entity inherits from the Gateways and
// GatewayClasses above it: the union, over the route being reconciled plus every route recorded in
// the live object's hybrid-routes annotations, of InheritedTagsForRoute.
//
// Reading the live object's attached-route list - rather than only the route being reconciled - is
// what makes a shared entity's tags identical for every writer. KongService, KongUpstream,
// KongTarget and the generated KongPlugin are deliberately shared by every route with the same
// backends and ControlPlane (see the comment on deduplicateOutputStore in the converter package),
// and they are all written under the same Server-Side Apply field manager. Resolving the tags from
// only one route would make two routes attached to differently tagged Gateways overwrite each
// other on every reconcile, and each write would re-trigger the other route. Contributing routes
// are sorted so the result is byte-identical regardless of which route is being reconciled.
//
// obj is used for its type and key only; the live copy is re-read from the cache.
func InheritedTagsForKongObject(
	ctx context.Context, logger logr.Logger, cl client.Client, route client.Object, obj client.Object,
) []string {
	// Resolve the reconciled route first, so its tags are memoized under its own key and the
	// loop below never re-reads the object we already hold.
	reconciled := inheritedTagsForRoute(ctx, logger, cl, route)

	var gatewayTags, gatewayClassTags []string
	for _, routeKey := range contributingRouteKeys(ctx, logger, cl, route, obj) {
		pair := reconciled
		if routeKey != routeCacheKey(route) {
			pair = inheritedTagsForRouteKey(ctx, logger, cl, routeKey)
		}
		gatewayTags = append(gatewayTags, pair.gateway...)
		gatewayClassTags = append(gatewayClassTags, pair.gatewayClass...)
	}
	// Every Gateway tag precedes every GatewayClass tag so that the documented drop order holds
	// even when several routes contribute.
	return append(gatewayTags, gatewayClassTags...)
}

// AppendTagsAnnotation appends tags to obj's konghq.com/tags annotation, after the tags already
// there, and leaves the annotation untouched when nothing is added. It is how tags reach a
// generated KongPlugin, whose tags live in that annotation rather than in a spec field (the
// Konnect ops layer reads them back with metadata.ExtractTags).
//
// Appending, rather than merging and re-sorting, is deliberate: the ops layer drops
// annotation-derived tags from the end when an entity is over the tag budget, so tags that must
// survive have to come first.
func AppendTagsAnnotation(logger logr.Logger, obj client.Object, tags []string) {
	if len(tags) == 0 {
		return
	}

	annotations := obj.GetAnnotations()
	var existing []string
	if current := annotations[pkgmetadata.AnnotationKeyTags]; current != "" {
		existing = strings.Split(current, ",")
	}

	merged := MergeTags(logger, existing, tags)
	if len(merged) == 0 {
		return
	}

	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[pkgmetadata.AnnotationKeyTags] = strings.Join(merged, ",")
	obj.SetAnnotations(annotations)
}

// TagsForGatewayClassOf returns the konghq.com/tags of the GatewayClass named by
// gw.Spec.GatewayClassName, or nil when the name is empty or the GatewayClass cannot be read.
// The Gateway converter uses this directly, since it already holds the Gateway.
func TagsForGatewayClassOf(ctx context.Context, logger logr.Logger, cl client.Client, gw *gwtypes.Gateway) []string {
	if gw == nil || gw.Spec.GatewayClassName == "" {
		return nil
	}
	name := string(gw.Spec.GatewayClassName)

	cache := tagCacheFrom(ctx)
	if cache != nil {
		if tags, ok := cache.gatewayClasses[name]; ok {
			return tags
		}
	}

	var tags []string
	gwc := &gwtypes.GatewayClass{}
	if err := cl.Get(ctx, client.ObjectKey{Name: name}, gwc); err != nil {
		log.Debug(logger, "Failed to fetch GatewayClass for tag inheritance",
			"gatewayClass", name, "error", err)
	} else {
		tags = pkgmetadata.ExtractTags(gwc)
	}

	if cache != nil {
		cache.gatewayClasses[name] = tags
	}
	return tags
}

// inheritedTagPair keeps Gateway tags apart from GatewayClass tags so that an aggregation over
// several routes can emit every Gateway tag before any GatewayClass tag.
type inheritedTagPair struct {
	gateway      []string
	gatewayClass []string
}

func inheritedTagsForRoute(ctx context.Context, logger logr.Logger, cl client.Client, route client.Object) inheritedTagPair {
	cacheKey := routeCacheKey(route)
	cache := tagCacheFrom(ctx)
	if cache != nil {
		if pair, ok := cache.routes[cacheKey]; ok {
			return pair
		}
	}

	var pair inheritedTagPair
	for _, pRef := range parentRefsOf(route) {
		parent := inheritedTagsForParentRef(ctx, logger, cl, route, pRef)
		pair.gateway = append(pair.gateway, parent.gateway...)
		pair.gatewayClass = append(pair.gatewayClass, parent.gatewayClass...)
	}

	if cache != nil {
		cache.routes[cacheKey] = pair
	}
	return pair
}

func inheritedTagsForParentRef(
	ctx context.Context, logger logr.Logger, cl client.Client,
	route client.Object, pRef gwtypes.ParentReference,
) inheritedTagPair {
	// Only Gateways in the Gateway API group can contribute tags, mirroring the parentRef
	// validation in the refs package.
	if pRef.Group != nil && *pRef.Group != gwtypes.GroupName {
		return inheritedTagPair{}
	}
	if pRef.Kind != nil && *pRef.Kind != "Gateway" {
		return inheritedTagPair{}
	}

	key := client.ObjectKey{Namespace: route.GetNamespace(), Name: string(pRef.Name)}
	if pRef.Namespace != nil && *pRef.Namespace != "" {
		key.Namespace = string(*pRef.Namespace)
	}

	cache := tagCacheFrom(ctx)
	if cache != nil {
		if pair, ok := cache.gateways[key]; ok {
			return pair
		}
	}

	var pair inheritedTagPair
	gw := &gwtypes.Gateway{}
	if err := cl.Get(ctx, key, gw); err != nil {
		// A Gateway that cannot be read contributes no tags rather than failing the translation.
		log.Debug(logger, "Failed to fetch parent Gateway for tag inheritance",
			"gateway", key, "error", err)
	} else {
		pair.gateway = pkgmetadata.ExtractTags(gw)
		pair.gatewayClass = TagsForGatewayClassOf(ctx, logger, cl, gw)
	}

	if cache != nil {
		cache.gateways[key] = pair
	}
	return pair
}

// contributingRouteKeys returns the key of the route being reconciled together with the key of
// every route recorded in the live copy of obj's hybrid-routes annotations, sorted so the caller's
// output does not depend on which route triggered the reconcile.
func contributingRouteKeys(
	ctx context.Context, logger logr.Logger, cl client.Client, route client.Object, obj client.Object,
) []string {
	keys := map[string]struct{}{routeCacheKey(route): {}}

	live, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return sortedKeys(keys)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), live); err != nil {
		// The Kong resource does not exist yet (or cannot be read): only the reconciled route
		// contributes. A later reconcile, triggered by the write itself, picks up the rest.
		log.Debug(logger, "Failed to read existing Kong resource for tag inheritance",
			"obj", client.ObjectKeyFromObject(obj), "error", err)
		return sortedKeys(keys)
	}

	am := metadata.NewAnnotationManager(logger)
	for _, kind := range supportedRouteKinds {
		for _, routeKey := range am.GetRoutesWithKind(live, kind) {
			keys[kind+"/"+routeKey] = struct{}{}
		}
	}

	return sortedKeys(keys)
}

// inheritedTagsForRouteKey resolves the inherited tags of a route identified by the key
// routeCacheKey produces. The route object is fetched only when its tags are not memoized yet, so
// the number of reads grows with the number of distinct routes rather than with the number of
// entities being translated.
func inheritedTagsForRouteKey(ctx context.Context, logger logr.Logger, cl client.Client, key string) inheritedTagPair {
	cache := tagCacheFrom(ctx)
	if cache != nil {
		if pair, ok := cache.routes[key]; ok {
			return pair
		}
	}

	kind, objectKey, ok := parseRouteCacheKey(key)
	if !ok {
		return inheritedTagPair{}
	}
	route := newRouteForKind(kind)
	if route == nil {
		return inheritedTagPair{}
	}
	if err := cl.Get(ctx, objectKey, route); err != nil {
		// A route that is already gone contributes no tags. Its entry in the hybrid-routes
		// annotation is pruned by the orphan handler on the next pass.
		log.Debug(logger, "Failed to fetch attached route for tag inheritance",
			"kind", kind, "route", objectKey, "error", err)
		if cache != nil {
			cache.routes[key] = inheritedTagPair{}
		}
		return inheritedTagPair{}
	}

	return inheritedTagsForRoute(ctx, logger, cl, route)
}

func sortedKeys(keys map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// routeCacheKey identifies a route by kind and object key. The kind comes from the concrete Go
// type rather than TypeMeta, which is not always populated on objects read from the cache.
func routeCacheKey(route client.Object) string {
	return routeKindOf(route) + "/" + client.ObjectKeyFromObject(route).String()
}

// parseRouteCacheKey is the inverse of routeCacheKey: "Kind/namespace/name".
func parseRouteCacheKey(key string) (kind string, objectKey client.ObjectKey, ok bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return "", client.ObjectKey{}, false
	}
	return parts[0], client.ObjectKey{Namespace: parts[1], Name: parts[2]}, true
}

func routeKindOf(route client.Object) string {
	switch route.(type) {
	case *gwtypes.HTTPRoute:
		return "HTTPRoute"
	case *gwtypes.GRPCRoute:
		return "GRPCRoute"
	case *gwtypes.TLSRoute:
		return "TLSRoute"
	case *gwtypes.TCPRoute:
		return "TCPRoute"
	case *gwtypes.UDPRoute:
		return "UDPRoute"
	}
	return route.GetObjectKind().GroupVersionKind().Kind
}

func newRouteForKind(kind string) client.Object {
	switch kind {
	case "HTTPRoute":
		return &gwtypes.HTTPRoute{}
	case "GRPCRoute":
		return &gwtypes.GRPCRoute{}
	case "TLSRoute":
		return &gwtypes.TLSRoute{}
	case "TCPRoute":
		return &gwtypes.TCPRoute{}
	case "UDPRoute":
		return &gwtypes.UDPRoute{}
	}
	return nil
}

func parentRefsOf(route client.Object) []gwtypes.ParentReference {
	switch r := route.(type) {
	case *gwtypes.HTTPRoute:
		return gwtypes.GetSpecParentRefs(*r)
	case *gwtypes.GRPCRoute:
		return gwtypes.GetSpecParentRefs(*r)
	case *gwtypes.TLSRoute:
		return gwtypes.GetSpecParentRefs(*r)
	case *gwtypes.TCPRoute:
		return gwtypes.GetSpecParentRefs(*r)
	case *gwtypes.UDPRoute:
		return gwtypes.GetSpecParentRefs(*r)
	}
	return nil
}

// tagCacheKeyType is the private context key type for the per-reconcile tag cache.
type tagCacheKeyType struct{}

// tagCache memoizes the Gateway, GatewayClass and route lookups performed while resolving
// inherited tags. Translation resolves the same handful of Gateways once per generated entity, and
// a shared entity additionally walks every route attached to it, so without memoization the number
// of (cached) reads grows with entities x attached routes x parentRefs.
type tagCache struct {
	gateways       map[client.ObjectKey]inheritedTagPair
	gatewayClasses map[string][]string
	routes         map[string]inheritedTagPair
}

// WithTagCache returns a context that memoizes inherited-tag lookups for the lifetime of one
// reconcile. It is safe to omit: the resolvers fall back to reading through the client directly.
// A reconcile is single-goroutine, so the cache needs no locking.
func WithTagCache(ctx context.Context) context.Context {
	if tagCacheFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, tagCacheKeyType{}, &tagCache{
		gateways:       map[client.ObjectKey]inheritedTagPair{},
		gatewayClasses: map[string][]string{},
		routes:         map[string]inheritedTagPair{},
	})
}

func tagCacheFrom(ctx context.Context) *tagCache {
	cache, _ := ctx.Value(tagCacheKeyType{}).(*tagCache)
	return cache
}
