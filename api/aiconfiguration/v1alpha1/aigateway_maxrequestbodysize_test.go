package v1alpha1

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// maxRequestBodySize documents 0 as "unlimited", so crd-from-oas config.yaml
// sets `_allowZero` on it and the field is generated as *int with omitempty: an
// explicit 0 must reach Konnect and on-prem AI Gateways instead of being dropped
// and replaced by the API's 8 MiB default.

// maxRequestBodySizeSDKDefault is the API default applied when the field is omitted.
const maxRequestBodySizeSDKDefault = 8388608

// TestMaxRequestBodySize_IsPointerEverywhere walks every type registered for
// this group and checks that each maxRequestBodySize field is *int with
// omitempty. It catches a config.yaml path that misses one of the (shared,
// per-variant) generated types, including ones added by a future OAS.
func TestMaxRequestBodySize_IsPointerEverywhere(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	visited := map[reflect.Type]bool{}
	var owners []string
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || typ.PkgPath() != reflect.TypeFor[AIGatewayAgent]().PkgPath() || visited[typ] {
			return
		}
		visited[typ] = true
		for field := range typ.Fields() {
			name, opts, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "maxRequestBodySize" {
				owners = append(owners, typ.Name())
				require.Equal(t, reflect.TypeFor[*int](), field.Type, "%s.%s", typ.Name(), field.Name)
				require.Contains(t, strings.Split(opts, ","), "omitempty", "%s.%s", typ.Name(), field.Name)
			}
			walk(field.Type)
		}
	}
	for gvk, typ := range scheme.AllKnownTypes() {
		if gvk.GroupVersion() == GroupVersion {
			walk(typ)
		}
	}

	// Guards the walker itself: these are the types known to carry the field today.
	require.Subset(t, owners, []string{
		"AIGatewayAgentConfig",
		"AIGatewayMCPServerConversionListenerConfig",
		"AIGatewayMCPServerListenerConfig",
		"AIGatewayMCPServerUpstreamServerConfig",
		"AIGatewayMCPServerWithUpstreamConfig",
		"AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig",
		"AIGatewayModelAPIConfig",
		"AIGatewayModelModelConfig",
	})
}

var maxRequestBodySizeCases = []struct {
	name  string
	value *int
}{
	{name: "unset", value: nil},
	{name: "explicit zero", value: new(0)},
	{name: "non-zero", value: new(1024)},
}

// requireSDKMaxRequestBodySize checks the value in a Konnect SDK request: unset
// falls back to the SDK default (8 MiB), not to 0 (unlimited).
func requireSDKMaxRequestBodySize(t *testing.T, want *int, got *int64) {
	t.Helper()
	require.NotNil(t, got)
	if want == nil {
		require.EqualValues(t, maxRequestBodySizeSDKDefault, *got)
		return
	}
	require.EqualValues(t, *want, *got)
}

func newMaxRequestBodySizeClient(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

func TestAIGatewayAgent_MaxRequestBodySize_Conversion(t *testing.T) {
	t.Parallel()

	for _, tc := range maxRequestBodySizeCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			obj := &AIGatewayAgent{
				Name: "sample-agent", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:   "sample-agent",
						Type:   "http",
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080", MaxRequestBodySize: tc.value},
					},
				},
			}
			cl := newMaxRequestBodySizeClient(t)

			req, err := obj.ToCreateAIGatewayAgentRequest(t.Context(), cl)
			require.NoError(t, err)
			requireSDKMaxRequestBodySize(t, tc.value, req.Config.MaxRequestBodySize)

			agent, err := obj.ToAIGWAgent(t.Context(), cl)
			require.NoError(t, err)
			require.Equal(t, tc.value, agent.Config.MaxRequestBodySize)
		})
	}
}

// TestAIGatewayMCPServer_MaxRequestBodySize_Conversion covers an entity whose
// apiSpec is a root union, which marshals through the union's own MarshalJSON
// rather than the plain struct path the Agent uses.
func TestAIGatewayMCPServer_MaxRequestBodySize_Conversion(t *testing.T) {
	t.Parallel()

	for _, tc := range maxRequestBodySizeCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			obj := &AIGatewayMCPServer{
				Name: "github-mcp", Namespace: "default",
				Spec: AIGatewayMCPServerSpec{
					APISpec: AIGatewayMCPServerAPISpec{
						AIGatewayMCPServerConfig: &AIGatewayMCPServerConfig{
							Type: AIGatewayMCPServerConfigTypePassthroughListener,
							PassthroughListener: &AIGatewayMCPServerPassthroughListener{
								Name:        "github-mcp",
								DisplayName: "GitHub MCP",
								Config: AIGatewayMCPServerWithUpstreamConfig{
									URL:                "https://api.githubcopilot.com/mcp",
									Route:              AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/github"}},
									MaxRequestBodySize: tc.value,
								},
							},
						},
					},
				},
			}
			cl := newMaxRequestBodySizeClient(t)

			req, err := obj.ToCreateAIGatewayMCPServerRequest(t.Context(), cl)
			require.NoError(t, err)
			require.NotNil(t, req.AIGatewayMCPServerPassthroughListener)
			cfg := req.AIGatewayMCPServerPassthroughListener.GetConfig()
			requireSDKMaxRequestBodySize(t, tc.value, cfg.GetMaxRequestBodySize())

			mcp, err := obj.ToAIGWMCPServer(t.Context(), cl)
			require.NoError(t, err)
			require.Equal(t, tc.value, mcp.Config.MaxRequestBodySize)
		})
	}
}
