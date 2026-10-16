package metadata

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// HybridRouteOwnerIndexField is the name of the field index over the
// hybrid-routes annotations of hybridgateway child resources.
//
// The hybrid-routes annotations are the source of truth for child ownership:
// a child resource can be shared by several root objects (e.g. multiple
// HTTPRoutes), which cannot be expressed with labels (label values cannot
// encode a set of owners). The index makes the per-annotation ownership
// queryable through the cached client, so reconcilers can list exactly the
// children of a single root object instead of listing every child carrying
// the (much broader) managed-by label.
const HybridRouteOwnerIndexField = "hybridRouteOwner"

// hybridRouteAnnotationKey associates each root-object kind with the
// hybrid-routes annotation key its children carry.
var hybridRouteAnnotationKeys = []struct {
	key  string
	kind string
}{
	{consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation, kindHTTPRoute},
	{consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation, kindGRPCRoute},
	{consts.GatewayOperatorHybridRoutesTLSRouteAnnotation, kindTLSRoute},
	{consts.GatewayOperatorHybridRoutesTCPRouteAnnotation, kindTCPRoute},
	{consts.GatewayOperatorHybridRoutesUDPRouteAnnotation, kindUDPRoute},
}

// HybridRouteOwnerIndexFunc is the field-index function for hybridgateway child
// resources. It emits one index value per route reference found in any of the
// hybrid-routes annotations, in "<RouteKind>/<namespace>/<name>" format, so a
// child shared by multiple routes is indexed under every owner.
func HybridRouteOwnerIndexFunc(obj client.Object) []string {
	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		return nil
	}
	var out []string
	for _, ka := range hybridRouteAnnotationKeys {
		value, ok := annotations[ka.key]
		if !ok || value == "" {
			continue
		}
		for entry := range strings.SplitSeq(value, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			out = append(out, ka.kind+"/"+entry)
		}
	}
	return out
}

// HybridRouteOwnerIndexKey returns the field-index key under which the child
// resources of the given root route object are indexed. It mirrors the
// "<RouteKind>/<namespace>/<name>" format produced by HybridRouteOwnerIndexFunc.
func HybridRouteOwnerIndexKey(route client.Object) string {
	return route.GetObjectKind().GroupVersionKind().Kind + "/" + client.ObjectKeyFromObject(route).String()
}

// hybridRouteOwnerChildType holds, per child resource kind, a constructor for
// the empty typed object used for index registration and one for the typed
// list used for indexed cached lists. Typed objects/lists are required because
// field selectors are only served by the cached client; unstructured lists
// bypass the cache. Constructors are used so each list call works on a fresh
// instance, safe under concurrent reconciles.
type hybridRouteOwnerChildType struct {
	newObject func() client.Object
	newList   func() client.ObjectList
}

// hybridRouteOwnerChildTypes maps the kinds of child resources managed by the
// hybridgateway converters to their typed object and list constructors.
var hybridRouteOwnerChildTypes = map[string]hybridRouteOwnerChildType{
	"KongRoute": {
		newObject: func() client.Object { return &configurationv1alpha1.KongRoute{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongRouteList{} },
	},
	"KongService": {
		newObject: func() client.Object { return &configurationv1alpha1.KongService{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongServiceList{} },
	},
	"KongUpstream": {
		newObject: func() client.Object { return &configurationv1alpha1.KongUpstream{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongUpstreamList{} },
	},
	"KongTarget": {
		newObject: func() client.Object { return &configurationv1alpha1.KongTarget{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongTargetList{} },
	},
	"KongPluginBinding": {
		newObject: func() client.Object { return &configurationv1alpha1.KongPluginBinding{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongPluginBindingList{} },
	},
	"KongSNI": {
		newObject: func() client.Object { return &configurationv1alpha1.KongSNI{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongSNIList{} },
	},
	"KongCertificate": {
		newObject: func() client.Object { return &configurationv1alpha1.KongCertificate{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongCertificateList{} },
	},
	"KongReferenceGrant": {
		newObject: func() client.Object { return &configurationv1alpha1.KongReferenceGrant{} },
		newList:   func() client.ObjectList { return &configurationv1alpha1.KongReferenceGrantList{} },
	},
	"KongPlugin": {
		newObject: func() client.Object { return &configurationv1.KongPlugin{} },
		newList:   func() client.ObjectList { return &configurationv1.KongPluginList{} },
	},
}

// RegisterHybridRouteOwnerIndexes registers HybridRouteOwnerIndexField for
// every child resource type that can carry hybrid-routes annotations. It must
// be called before the manager is started. It is idempotent: registering the
// same index several times (e.g. once per root-object controller sharing one
// manager) is not an error.
func RegisterHybridRouteOwnerIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	kinds := make([]string, 0, len(hybridRouteOwnerChildTypes))
	for kind := range hybridRouteOwnerChildTypes {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	for _, kind := range kinds {
		if err := indexer.IndexField(ctx, hybridRouteOwnerChildTypes[kind].newObject(), HybridRouteOwnerIndexField, HybridRouteOwnerIndexFunc); err != nil {
			// The same index is registered once per root-object controller on a
			// shared manager; a conflict means it is already registered, which
			// is fine (both the informer cache and the fake client report it).
			if isIndexAlreadyRegisteredError(err) {
				continue
			}
			return fmt.Errorf("failed to register %s index for %s: %w", HybridRouteOwnerIndexField, kind, err)
		}
	}
	return nil
}

// isIndexAlreadyRegisteredError reports whether err indicates that the field
// index has already been registered for the type.
func isIndexAlreadyRegisteredError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// client-go informer cache: "indexer conflict: <name>";
	// fake client: "...index with name <name> has already been registered...".
	return strings.Contains(msg, "indexer conflict") || strings.Contains(msg, "already registered")
}

// ListOwnedChildResources lists the child resources of the given root object
// for the expected GVK. When a typed list and a registered field index are
// available for the GVK, the list is served from the cache and scoped to the
// root object via the hybrid-routes annotation index; otherwise it falls back
// to a broad live unstructured list filtered by the managed-by label (legacy
// behavior). The result is always returned as unstructured items so callers
// can treat all child GVKs uniformly.
func ListOwnedChildResources(
	ctx context.Context,
	cl client.Client,
	rootObj client.Object,
	gvk schema.GroupVersionKind,
) (*unstructured.UnstructuredList, error) {
	if childType, ok := hybridRouteOwnerChildTypes[gvk.Kind]; ok {
		listObj := childType.newList()
		labelsListOpt := client.MatchingFields{
			HybridRouteOwnerIndexField: HybridRouteOwnerIndexKey(rootObj),
		}
		if err := cl.List(ctx, listObj, labelsListOpt); err == nil {
			return toUnstructuredList(listObj, gvk)
		} else {
			// The indexed cached path is unavailable in this client (field
			// index not registered for it, e.g. plain fake clients in unit
			// tests, or the list type missing from the client's scheme); fall
			// back to the legacy broad live list rather than failing the
			// reconcile. Production managers register the index, so the fast
			// path is used there.
			ctrllog.FromContext(ctx).V(1).Info("hybrid-route owner index unavailable, falling back to managed-by label listing",
				"gvk", gvk.String(), "error", err.Error())
		}
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)
	if err := cl.List(ctx, list, LabelSelectorForOwnedResources(rootObj, nil)); err != nil {
		return nil, fmt.Errorf("unable to list objects with gvk %s: %w", gvk.String(), err)
	}
	return list, nil
}

// toUnstructuredList converts a typed list returned by the cached client into
// an unstructured list so the generic orphan-cleanup and status logic can
// handle every child GVK uniformly.
func toUnstructuredList(listObj client.ObjectList, gvk schema.GroupVersionKind) (*unstructured.UnstructuredList, error) {
	items, err := meta.ExtractList(listObj)
	if err != nil {
		return nil, fmt.Errorf("failed to extract items from list of gvk %s: %w", gvk.String(), err)
	}
	out := &unstructured.UnstructuredList{}
	for _, item := range items {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(item)
		if err != nil {
			return nil, fmt.Errorf("failed to convert %T to unstructured: %w", item, err)
		}
		u := unstructured.Unstructured{Object: m}
		u.SetGroupVersionKind(gvk)
		out.Items = append(out.Items, u)
	}
	return out, nil
}
