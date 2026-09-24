package converter

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// tagsFixture wires up the standard Konnect Gateway objects plus a backend Service and its
// EndpointSlice, with the given tags annotations on the Gateway, the GatewayClass, the HTTPRoute
// and the backend Service.
type tagsFixture struct {
	routeTags, gatewayTags, gatewayClassTags, serviceTags string
}

func (f tagsFixture) build(t *testing.T) (*gwtypes.HTTPRoute, client.Client) {
	t.Helper()

	route := newHTTPRouteWithRules(nil, []gwtypes.HTTPRouteRule{{
		Matches: []gwtypes.HTTPRouteMatch{{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  new(gatewayv1.PathMatchExact),
				Value: new("/one"),
			},
		}},
		BackendRefs: []gwtypes.HTTPBackendRef{newBackendRef("")},
	}})
	if f.routeTags != "" {
		route.Annotations = map[string]string{"konghq.com/tags": f.routeTags}
	}

	gateway := newGatewayWithListenerHostnames()
	gateway.UID = types.UID("gateway-uid")
	if f.gatewayTags != "" {
		gateway.Annotations = map[string]string{"konghq.com/tags": f.gatewayTags}
	}

	backendService := newService("default")
	if f.serviceTags != "" {
		backendService.Annotations = map[string]string{"konghq.com/tags": f.serviceTags}
	}

	objects := append(
		newKonnectGatewayStandardObjects(gateway),
		backendService,
		newEndpointSlice("backend-service", "default", []string{"10.0.1.1"}),
	)
	// newKonnectGatewayStandardObjects already supplies the GatewayClass the Gateway points at.
	if f.gatewayClassTags != "" {
		for _, obj := range objects {
			if gwc, ok := obj.(*gwtypes.GatewayClass); ok {
				gwc.Annotations = map[string]string{"konghq.com/tags": f.gatewayClassTags}
			}
		}
	}
	return route, fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
}

// tagsByKind collects the spec.tags of every object a translation produced, keyed by kind.
func tagsByKind(t *testing.T, outputStore []client.Object) map[string]commonv1alpha1.Tags {
	t.Helper()

	byKind := map[string]commonv1alpha1.Tags{}
	for _, obj := range outputStore {
		switch o := obj.(type) {
		case *configurationv1alpha1.KongRoute:
			byKind["KongRoute"] = o.Spec.Tags
		case *configurationv1alpha1.KongService:
			byKind["KongService"] = o.Spec.Tags
		case *configurationv1alpha1.KongUpstream:
			byKind["KongUpstream"] = o.Spec.Tags
		case *configurationv1alpha1.KongTarget:
			byKind["KongTarget"] = o.Spec.Tags
		}
	}
	return byKind
}

// TestHTTPRouteConverter_TranslateInheritsGatewayTags walks the whole translation and checks that
// every generated Kong entity carries its own tags first and the tags inherited from the parent
// Gateway and its GatewayClass after, so the least specific tags are dropped first when the set
// goes over the CRD's tag budget.
func TestHTTPRouteConverter_TranslateInheritsGatewayTags(t *testing.T) {
	tests := []struct {
		name     string
		fixture  tagsFixture
		expected map[string]commonv1alpha1.Tags
	}{
		{
			name: "every entity inherits the Gateway and GatewayClass tags after its own",
			fixture: tagsFixture{
				routeTags:        "route-tag",
				gatewayTags:      "gw-tag",
				gatewayClassTags: "class-tag",
				serviceTags:      "svc-tag",
			},
			expected: map[string]commonv1alpha1.Tags{
				"KongRoute":    {"route-tag", "gw-tag", "class-tag"},
				"KongService":  {"svc-tag", "gw-tag", "class-tag"},
				"KongUpstream": {"svc-tag", "gw-tag", "class-tag"},
				"KongTarget":   {"svc-tag", "gw-tag", "class-tag"},
			},
		},
		{
			name:    "tags on the Gateway alone reach every entity",
			fixture: tagsFixture{gatewayTags: "gw-tag"},
			expected: map[string]commonv1alpha1.Tags{
				"KongRoute":    {"gw-tag"},
				"KongService":  {"gw-tag"},
				"KongUpstream": {"gw-tag"},
				"KongTarget":   {"gw-tag"},
			},
		},
		{
			name:    "tags on the GatewayClass alone reach every entity",
			fixture: tagsFixture{gatewayClassTags: "class-tag"},
			expected: map[string]commonv1alpha1.Tags{
				"KongRoute":    {"class-tag"},
				"KongService":  {"class-tag"},
				"KongUpstream": {"class-tag"},
				"KongTarget":   {"class-tag"},
			},
		},
		{
			name:    "no tags anywhere leaves every entity untagged",
			fixture: tagsFixture{},
			expected: map[string]commonv1alpha1.Tags{
				"KongRoute":    nil,
				"KongService":  nil,
				"KongUpstream": nil,
				"KongTarget":   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, cl := tt.fixture.build(t)

			converter := newHTTPRouteConverter(route, cl, false, "", testReferenceGrantVersion)
			_, err := converter.Translate(t.Context(), logr.Discard())
			require.NoError(t, err)

			got := tagsByKind(t, converter.(*httpRouteConverter).outputStore)
			require.Len(t, got, len(tt.expected), "expected one object of each kind")
			for kind, want := range tt.expected {
				assert.Equal(t, want, got[kind], "unexpected tags on %s", kind)
			}
		})
	}
}

// TestHTTPRouteConverter_TranslateTruncatesInheritedTags pins that going over the tag budget drops
// the least specific tags rather than producing a spec.tags the API server would reject:
// commonv1alpha1.Tags carries MaxItems=20.
func TestHTTPRouteConverter_TranslateTruncatesInheritedTags(t *testing.T) {
	var routeTags []string
	for i := range utils.MaxTags - 2 {
		routeTags = append(routeTags, fmt.Sprintf("route-%02d", i))
	}

	fixture := tagsFixture{
		routeTags:        joinTags(routeTags),
		gatewayTags:      "gw-a,gw-b,gw-c",
		gatewayClassTags: "class-a",
	}
	route, cl := fixture.build(t)

	converter := newHTTPRouteConverter(route, cl, false, "", testReferenceGrantVersion)
	_, err := converter.Translate(t.Context(), logr.Discard())
	require.NoError(t, err)

	kongRouteTags := tagsByKind(t, converter.(*httpRouteConverter).outputStore)["KongRoute"]
	require.Len(t, kongRouteTags, utils.MaxTags)
	// The route's own tags all survive; the Gateway's fill the remaining two slots and the
	// GatewayClass's are dropped first.
	assert.Equal(t, commonv1alpha1.Tags(append(append([]string{}, routeTags...), "gw-a", "gw-b")), kongRouteTags)
	assert.NotContains(t, kongRouteTags, "gw-c")
	assert.NotContains(t, kongRouteTags, "class-a")
}

func joinTags(tags []string) string {
	out := ""
	for i, tag := range tags {
		if i > 0 {
			out += ","
		}
		out += tag
	}
	return out
}
