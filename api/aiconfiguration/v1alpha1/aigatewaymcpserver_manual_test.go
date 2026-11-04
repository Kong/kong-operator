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
								Acls: &AIGatewayMCPServerListenerConsumerAcls{
									Type:  AIGatewayMCPServerListenerConsumerAclsTypeAllow,
									Allow: []AIGatewayACLRef{{Name: "server-allow"}},
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
										Deny: []AIGatewayACLRef{{Name: "deny-group"}},
									},
								},
							},
							{
								Name:        "t1",
								Description: "t1",
								Method:      "GET",
								Access: AIGatewayMCPToolAccess{
									Acls: AIGatewayMCPACLs{
										Allow: []AIGatewayACLRef{{Name: "allow-group"}},
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

// TestAIGatewayMCPServer_ServerACLsArms guards that every server-level ACL arm
// reaches the SDK create and update requests with its references resolved, and
// that a CR written in the pre-union shape (no "type") keeps its ACLs.
func TestAIGatewayMCPServer_ServerACLsArms(t *testing.T) {
	t.Parallel()

	newGroup := func(name, konnectName string) *AIGatewayConsumerGroup {
		group := &AIGatewayConsumerGroup{
			Name: name, Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{Name: AIGatewayEntityIdentifier(konnectName)},
			},
		}
		group.SetKonnectID(name + "-kid")
		return group
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(newGroup("allow-group", "konnect-allow"), newGroup("deny-group", "konnect-deny")).
		Build()

	newObj := func(acls *AIGatewayMCPServerListenerConsumerAcls) *AIGatewayMCPServer {
		return &AIGatewayMCPServer{
			Name: "mcp", Namespace: "default",
			Spec: AIGatewayMCPServerSpec{APISpec: AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &AIGatewayMCPServerConfig{
					Type: AIGatewayMCPServerConfigTypeListener,
					Listener: &AIGatewayMCPServerListener{
						Name:        "mcp",
						DisplayName: "MCP",
						Access: &AIGatewayMCPServerListenerAccess{
							AclAttributeType: AIGatewayMCPServerListenerAccessTypeConsumer,
							Consumer:         &AIGatewayMCPServerListenerConsumer{Acls: acls},
						},
						Config: AIGatewayMCPServerListenerConfig{
							Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp"}},
						},
					},
				},
			}},
		}
	}

	tests := []struct {
		name string
		acls *AIGatewayMCPServerListenerConsumerAcls
		want map[string]any
	}{
		{
			name: "allow",
			acls: &AIGatewayMCPServerListenerConsumerAcls{
				Type:  AIGatewayMCPServerListenerConsumerAclsTypeAllow,
				Allow: []AIGatewayACLRef{{Name: "allow-group"}},
			},
			want: map[string]any{"allow": []any{"konnect-allow"}},
		},
		{
			name: "deny",
			acls: &AIGatewayMCPServerListenerConsumerAcls{
				Type: AIGatewayMCPServerListenerConsumerAclsTypeDeny,
				Deny: []AIGatewayACLRef{{Name: "deny-group"}},
			},
			want: map[string]any{"deny": []any{"konnect-deny"}},
		},
		{
			name: "allowWhen",
			acls: &AIGatewayMCPServerListenerConsumerAcls{
				Type:      AIGatewayMCPServerListenerConsumerAclsTypeAllowWhen,
				AllowWhen: []string{"has(principal)"},
			},
			want: map[string]any{"allow_when": []any{"has(principal)"}},
		},
		{
			name: "denyWhen",
			acls: &AIGatewayMCPServerListenerConsumerAcls{
				Type:     AIGatewayMCPServerListenerConsumerAclsTypeDenyWhen,
				DenyWhen: []string{"has(principal)"},
			},
			want: map[string]any{"deny_when": []any{"has(principal)"}},
		},
		{
			name: "pre-union shape without a type",
			acls: &AIGatewayMCPServerListenerConsumerAcls{
				Allow: []AIGatewayACLRef{{Name: "allow-group"}},
			},
			want: map[string]any{"allow": []any{"konnect-allow"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj := newObj(tt.acls)
			createReq, err := obj.ToCreateAIGatewayMCPServerRequest(t.Context(), cl)
			require.NoError(t, err)
			updateReq, err := obj.ToUpdateAIGatewayMCPServerRequest(t.Context(), cl)
			require.NoError(t, err)

			for op, req := range map[string]any{"create": createReq, "update": updateReq} {
				data, err := json.Marshal(req)
				require.NoError(t, err)

				var decoded map[string]any
				require.NoError(t, json.Unmarshal(data, &decoded))
				got, ok := decoded["access"].(map[string]any)["acls"].(map[string]any)
				require.True(t, ok, "%s request has no access.acls", op)
				require.Equal(t, tt.want, got, "%s request ACLs", op)
			}
		})
	}
}
