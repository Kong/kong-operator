package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayMCPServer_MixedToolAclsInjection guards the alignment invariant
// between the per-tool ACL reference accessors (which skip tools without the
// relevant allow/deny list) and the SDK payload injection (which skips payload
// elements without the relevant key): each tool must receive its own resolved
// list, never a sibling tool's. A regression in either skip rule would write
// tool A's resolved ACL list onto tool B.
func TestAIGatewayMCPServer_MixedToolAclsInjection(t *testing.T) {
	t.Parallel()

	newConsumerGroup := func(name, konnectName string) *AIGatewayConsumerGroup {
		return &AIGatewayConsumerGroup{
			Name: name, Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{
					Name: AIGatewayEntityIdentifier(konnectName),
				},
			},
		}
	}

	obj := &AIGatewayMCPServer{
		Name: "mcp", Namespace: "default",
		Spec: AIGatewayMCPServerSpec{
			APISpec: AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &AIGatewayMCPServerConfig{
					Type: AIGatewayMCPServerConfigTypeConversionListener,
					ConversionListener: &AIGatewayMCPServerConversionListener{
						Name:        "mcp",
						DisplayName: "MCP",
						Config: AIGatewayMCPServerConversionListenerConfig{
							URL: "https://example.com/mcp",
						},
						Access: &AIGatewayMCPServerConversionListenerAccess{
							AclAttributeType: AIGatewayMCPServerConversionListenerAccessTypeConsumer,
							Consumer: &AIGatewayMCPServerListenerConsumer{
								// Server-level allow list must not bleed into (or
								// consume resolved values of) the per-tool lists.
								Acls: AIGatewayMCPACLs{
									Allow: []AIGatewayMCPACLRef{{Name: "server-allow"}},
								},
							},
						},
						// t0 carries only a deny list, t1 only an allow list:
						// either skip rule regressing misaligns the positional
						// injection and swaps the two tools' lists.
						Tools: []AIGatewayMCPConversionTool{
							{
								Name:        "t0",
								Description: "t0",
								Method:      "GET",
								Access: AIGatewayMCPToolAccess{
									Acls: AIGatewayMCPACLs{
										Deny: []AIGatewayMCPACLRef{{Name: "deny-group"}},
									},
								},
							},
							{
								Name:        "t1",
								Description: "t1",
								Method:      "GET",
								Access: AIGatewayMCPToolAccess{
									Acls: AIGatewayMCPACLs{
										Allow: []AIGatewayMCPACLRef{{Name: "allow-group"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	denyGroup := newConsumerGroup("deny-group", "konnect-deny")
	denyGroup.SetKonnectID("deny-group-kid")
	allowGroup := newConsumerGroup("allow-group", "konnect-allow")
	allowGroup.SetKonnectID("allow-group-kid")
	serverAllowGroup := newConsumerGroup("server-allow", "konnect-server-allow")
	serverAllowGroup.SetKonnectID("server-allow-kid")
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(denyGroup, allowGroup, serverAllowGroup).Build()

	req, err := obj.ToCreateAIGatewayMCPServerRequest(t.Context(), cl)
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded struct {
		Tools []struct {
			Name   string `json:"name"`
			Access struct {
				Acls struct {
					Allow []string `json:"allow,omitempty"`
					Deny  []string `json:"deny,omitempty"`
				} `json:"acls"`
			} `json:"access"`
		} `json:"tools"`
		Access struct {
			Acls struct {
				Allow []string `json:"allow,omitempty"`
			} `json:"acls"`
		} `json:"access"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))

	// Each tool got exactly its own list, resolved to the referenced CR's
	// Konnect name.
	require.Len(t, decoded.Tools, 2)
	require.Equal(t, "t0", decoded.Tools[0].Name)
	require.Empty(t, decoded.Tools[0].Access.Acls.Allow)
	require.Equal(t, []string{"konnect-deny"}, decoded.Tools[0].Access.Acls.Deny)
	require.Equal(t, "t1", decoded.Tools[1].Name)
	require.Equal(t, []string{"konnect-allow"}, decoded.Tools[1].Access.Acls.Allow)
	require.Empty(t, decoded.Tools[1].Access.Acls.Deny)

	// The server-level list resolved independently.
	require.Equal(t, []string{"konnect-server-allow"}, decoded.Access.Acls.Allow)
}
