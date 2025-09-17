package converter

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

var _ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	httpRoute       *gwtypes.HTTPRoute
	outputStore     []configurationv1alpha1.KongRoute
	sharedStatusMap *route.SharedRouteStatusMap
	context         *httpRouteTranslationContext
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func NewHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client, sharedStatusMap *route.SharedRouteStatusMap) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:          cl,
		outputStore:     []configurationv1alpha1.KongRoute{},
		sharedStatusMap: sharedStatusMap,
		httpRoute:       httpRoute,
	}
}

// GetRootObject implements APIConverter.
func (c *httpRouteConverter) GetRootObject() gwtypes.HTTPRoute {
	return *c.httpRoute
}

// Translate implements APIConverter.
func (c *httpRouteConverter) Translate() error {
	return c.translate()
}

// GetOutputStore implements APIConverter.
func (c *httpRouteConverter) GetOutputStore(ctx context.Context) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(c.outputStore))
	for _, kr := range c.outputStore {
		unstr, err := utils.ToUnstructured(&kr, c.Scheme())
		if err != nil {
			continue
		}
		objects = append(objects, unstr)
	}
	return objects
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
	if c.httpRoute == nil {
		return nil, nil
	}

	list := &configurationv1alpha1.KongRouteList{}
	labels := map[string]string{
		// TODO: Add appropriate labels for KongRoute objects managed by HTTPRoute
	}
	opts := []client.ListOption{
		client.InNamespace(c.httpRoute.Namespace),
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
func (c *httpRouteConverter) translate() error {
	for _, parentRef := range c.httpRoute.Spec.ParentRefs {
		kbr := NewKongRouteBuilder().WithHosts(c.context.getHostnamesForParentRef(string(parentRef.Name)))
		for _, rule := range c.httpRoute.Spec.Rules {
			for i, match := range rule.Matches {
				// Get the corresponding KongService for this parentRef and match index.
				if ks := c.context.getKongServiceForParentRefAndMatch(string(parentRef.Name), i); ks != nil {
					kbr = kbr.WithKongService(ks)
				}
				kbr = kbr.WithHTTPRouteMatch(match).WithOwner(c.httpRoute).WithMetadata(c.httpRoute, &parentRef, &match)
			}
		}
		c.outputStore = append(c.outputStore, kbr.Build())
	}
	return nil
}

// HTTPRouteTranslationContext holds all data needed to translate an HTTPRoute to KongRoutes.
type httpRouteTranslationContext struct {
	// Keyed by parentRef (namespace/name)
	hostnamesPerParentRef map[string][]string
	// Maps each parentRef (gateway/control plane, as "namespace/name") to a slice of KongServices,
	// where the slice index corresponds to the index of the HTTPRouteMatch in HTTPRoute.Spec.Rules.Matches.
	// This enables efficient lookup of the KongService for a given parentRef and match, maintaining
	// a stable relationship between HTTPRoute matches and their associated KongServices. The mapping
	// remains valid as HTTPRoute matches do not change during reconciliation.
	kongServiceForParentRefAndMatch map[string][]*configurationv1alpha1.KongService
}

func (ctx *httpRouteTranslationContext) getHostnamesForParentRef(parentRef string) []string {
	if ctx == nil || ctx.hostnamesPerParentRef == nil {
		return nil
	}
	return ctx.hostnamesPerParentRef[parentRef]
}

func (ctx *httpRouteTranslationContext) getKongServiceForParentRefAndMatch(parentRef string, matchIndex int) *configurationv1alpha1.KongService {
	if ctx == nil || ctx.kongServiceForParentRefAndMatch == nil {
		return nil
	}
	services, exists := ctx.kongServiceForParentRefAndMatch[parentRef]
	if !exists || matchIndex < 0 || matchIndex >= len(services) {
		return nil
	}
	return services[matchIndex]
}

// BuildHTTPRouteTranslationContext builds a translation context for a given HTTPRoute.
func BuildHTTPRouteTranslationContext(ctx context.Context, httpRoute *gwtypes.HTTPRoute, cl client.Client) (*httpRouteTranslationContext, error) {
	// TODO: Gather backendRefs, resolve KongServices, Gateways, and hostnames.
	// Populate HTTPRouteTranslationContext with concrete types.
	return &httpRouteTranslationContext{}, nil
}
