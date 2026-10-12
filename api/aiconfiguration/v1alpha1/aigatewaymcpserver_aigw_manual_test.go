package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The want values are decoded from aigw-shaped YAML literals: aigw/doc.go doesn't alias
// MCPServerConfig, MCPTool, MCPAccess, ProxyConfig or UpstreamConfig, so an expected value
// cannot be constructed field by field for the types it would need (same constraint as the
// agent/model tests, worked around by decoding instead of composing).

// wantMCPServerFromYAML decodes an expected aigw.MCPServer from its YAML literal.
func wantMCPServerFromYAML(t *testing.T, literal string) *aigw.MCPServer {
	t.Helper()
	var m aigw.MCPServer
	require.NoError(t, yaml.Unmarshal([]byte(literal), &m))
	return &m
}

// TestAIGatewayMCPServer_ToAIGWMCPServer covers every config variant except
// conversion-listener (only its payload marshaling is covered by the strict round-trip
// test below), the CR reference resolution (deliberately without any SetKonnectID on the
// referenced entities, pinning that the on-prem translation uses the name-only resolver),
// the config.url -> upstream_url move, the Enabled/Disabled boolean normalization and the
// deprecated identity_providers folding into AuthStrategies. (The managed_by drop and the
// cache/allowed_versions drop are pinned by the strict round-trip test below, the only
// decode that can see those keys.)
func TestAIGatewayMCPServer_ToAIGWMCPServer(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferenced* return fresh objects per subtest: the fake client's tracker mutates the
	// objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newReferencedPolicy := func() *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Name: "ai-gw-policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name: "aigw-policy",
					Type: "rate-limiting",
				},
			},
		}
	}
	newReferencedConsumerGroup := func() *AIGatewayConsumerGroup {
		return &AIGatewayConsumerGroup{
			Name: "ai-gw-consumer-group", Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{
					Name: "dev-users",
				},
			},
		}
	}
	newReferencedDeniedConsumerGroup := func() *AIGatewayConsumerGroup {
		return &AIGatewayConsumerGroup{
			Name: "ai-gw-denied-consumer-group", Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{
					Name: "denied-users",
				},
			},
		}
	}
	newReferencedAuthStrategy := func() *AIGatewayAuthStrategy {
		return &AIGatewayAuthStrategy{
			Name: "ai-gw-auth-strategy", Namespace: "default",
			Spec: AIGatewayAuthStrategySpec{
				APISpec: AIGatewayAuthStrategyAPISpec{
					AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
						Type:    AIGatewayAuthStrategyConfigTypeKeyAuth,
						KeyAuth: &AIGatewayAuthStrategyKeyAuth{Name: "key-auth-1"},
					},
				},
			},
		}
	}
	newReferencedSourceMCPServer := func() *AIGatewayMCPServer {
		return &AIGatewayMCPServer{
			Name: "ai-gw-source-mcp-server", Namespace: "default",
			Spec: AIGatewayMCPServerSpec{
				APISpec: AIGatewayMCPServerAPISpec{
					AIGatewayMCPServerConfig: &AIGatewayMCPServerConfig{
						Type: AIGatewayMCPServerConfigTypeConversionOnly,
						ConversionOnly: &AIGatewayMCPServerConversionOnly{
							Name: "flights-toolset",
							Config: AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{
								URL: "http://flights:8080",
							},
						},
					},
				},
			},
		}
	}

	newMCPServer := func(config *AIGatewayMCPServerConfig) *AIGatewayMCPServer {
		return &AIGatewayMCPServer{
			Name: "sample-ai-gw-mcp-server", Namespace: "default",
			Spec: AIGatewayMCPServerSpec{APISpec: AIGatewayMCPServerAPISpec{AIGatewayMCPServerConfig: config}},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayMCPServer
		objects []runtime.Object
		want    string // aigw.MCPServer YAML literal
		wantErr string
	}{
		{
			name: "conversion-only: config.url moved to upstream_url, tools and their ACLs and policies resolved, bool enums normalized",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeConversionOnly,
				ConversionOnly: &AIGatewayMCPServerConversionOnly{
					Name:        "flights",
					DisplayName: "Flights MCP Server",
					Enabled:     "Enabled",
					Labels:      PublicLabels{"app": "test1"},
					ManagedBy:   ManagedBy{"kong-operator": "true"},
					Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					Config: AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{
						URL:   "http://upstream:8080",
						Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/flights"}, StripPath: "Enabled"},
						Logging: AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfigLogging{
							Payloads: "Disabled",
						},
						MaxRequestBodySize: 1024,
					},
					Tools: []AIGatewayMCPConversionTool{{
						Name:        "search_flights",
						Description: "Search flights",
						Method:      "GET",
						Path:        "/flights",
						Annotations: AIGatewayMCPToolAnnotations{Title: "Search flights", DestructiveHint: "Disabled"},
						Headers:     AIGatewayMCPToolHeaders{"x-tenant": "acme"},
						Parameters: []AIGatewayMCPToolParameter{{
							Name:     "date",
							In:       "query",
							Required: "Enabled",
							Schema:   apiextensionsv1.JSON{Raw: []byte(`{"type":"string"}`)},
						}},
						Access: AIGatewayMCPToolAccess{
							Acls: AIGatewayMCPACLs{Allow: []AIGatewayMCPACLRef{{Name: "ai-gw-consumer-group"}}},
						},
					}},
				},
			}),
			objects: []runtime.Object{newReferencedPolicy(), newReferencedConsumerGroup()},
			want: `
type: conversion-only
name: flights
display_name: Flights MCP Server
enabled: true
labels: {app: test1}
upstream_url: http://upstream:8080
policies: [aigw-policy]
config:
  route:
    paths: [/mcp/flights]
    strip_path: true
  logging:
    payloads: false
  max_request_body_size: 1024
tools:
- name: search_flights
  description: Search flights
  method: GET
  path: /flights
  annotations: {title: Search flights, destructive_hint: false}
  headers: {x-tenant: acme}
  parameters:
  - name: date
    in: query
    required: true
    schema: {"type": "string"}
  access:
    acls: {allow: [dev-users]}
`,
		},
		{
			name: "listener: sources resolved, consumer access with server and default-tool ACLs, auth strategies, identity_providers folded; cache and allowed_versions dropped",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeListener,
				Listener: &AIGatewayMCPServerListener{
					Name:        "mcp-listener",
					DisplayName: "MCP Listener",
					Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					Sources:     []AIGatewayMCPServerRef{{Name: "ai-gw-source-mcp-server"}},
					Access: &AIGatewayMCPServerListenerAccess{
						AclAttributeType: AIGatewayMCPServerListenerAccessTypeConsumer,
						Consumer: &AIGatewayMCPServerListenerConsumer{
							Acls: AIGatewayMCPACLs{
								Allow: []AIGatewayMCPACLRef{{Name: "ai-gw-consumer-group"}},
								Deny:  []AIGatewayMCPACLRef{{Name: "ai-gw-denied-consumer-group"}},
							},
							DefaultToolAcls: AIGatewayMCPACLs{
								Allow: []AIGatewayMCPACLRef{{Name: "ai-gw-consumer-group"}},
							},
							AuthStrategies:    []AIGatewayAuthStrategyRef{{Name: "ai-gw-auth-strategy"}},
							IdentityProviders: []AIGatewayIdentityProviderReference{"legacy-idp"},
						},
					},
					Config: AIGatewayMCPServerListenerConfig{
						Route: AIGatewayMCPServerRouteWithMatcher{
							Paths:   []string{"/mcp"},
							Methods: []string{"POST"},
							Headers: apiextensionsv1.JSON{Raw: []byte(`{"x-gateway":["kong"]}`)},
						},
						Server: AIGatewayMCPServerServerConfigBase{
							ForwardClientHeaders: "Enabled",
							Session: AIGatewayMCPServerServerConfigBaseSession{
								Managed:    "Enabled",
								Strategy:   "client",
								SessionTtl: 60,
								Client: AIGatewayMCPServerServerConfigBaseSessionClient{
									Secrets: []string{"s1"},
								},
							},
						},
						AllowedVersions: []string{"2025-06-18"},
						Cache: AIGatewayMCPServerListenerConfigCache{
							ToolsList: AIGatewayMCPServerCacheHint{CacheScope: "public", TtlMs: 5000},
						},
					},
				},
			}),
			objects: []runtime.Object{newReferencedPolicy(), newReferencedConsumerGroup(), newReferencedDeniedConsumerGroup(), newReferencedAuthStrategy(), newReferencedSourceMCPServer()},
			want: `
type: listener
name: mcp-listener
display_name: MCP Listener
policies: [aigw-policy]
config:
  route:
    paths: [/mcp]
    methods: [POST]
    headers: {x-gateway: [kong]}
  server:
    forward_client_headers: true
    # The session's strategy variants (client/redis) flatten into the session block by the
    # SDK union semantics the Konnect payload follows: the strategy discriminator stays and
    # the selected variant's fields (here client.secrets) land inline.
    session:
      managed: true
      strategy: client
      session_ttl: 60
      secrets: [s1]
  sources: [flights-toolset]
access:
  acl_attribute_type: consumer
  acls: {allow: [dev-users], deny: [denied-users]}
  default_tool_acls: {allow: [dev-users]}
  auth_strategies: [key-auth-1, legacy-idp]
`,
		},
		{
			name: "passthrough-listener: oauth access with claim field and metadata, proxy and upstream auth, passthrough tools",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypePassthroughListener,
				PassthroughListener: &AIGatewayMCPServerPassthroughListener{
					Name:        "github-mcp",
					DisplayName: "GitHub MCP",
					Enabled:     "Disabled",
					Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					Access: &AIGatewayMCPServerPassthroughListenerAccess{
						AclAttributeType: AIGatewayMCPServerPassthroughListenerAccessTypeOauth,
						Oauth: &AIGatewayMCPServerListenerOauth{
							AccessTokenClaimField: ".user.email",
							Acls: AIGatewayMCPACLs{
								Deny: []AIGatewayMCPACLRef{{Name: "ai-gw-denied-consumer-group"}},
							},
							AuthStrategies: []AIGatewayAuthStrategyRef{{Name: "ai-gw-auth-strategy"}},
							Metadata: AIGatewayMCPServerProtectedResourceMetadata{
								Resource:             "https://mcp.example.com/github",
								Endpoint:             "/.well-known/oauth-protected-resource",
								DiscoveryEndpoint:    "/.well-known/oauth-authorization-server",
								AuthorizationServers: []string{"https://auth.example.com"},
								ScopesSupported:      []string{"repo"},
							},
						},
					},
					Config: AIGatewayMCPServerWithUpstreamConfig{
						URL:   "https://api.githubcopilot.com/mcp",
						Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/github-mcp"}},
						Proxy: AIGatewayProxyConfig{
							HTTPProxy: AIGatewayProxyConfigHTTPProxy{Host: "proxy.local", Port: 3128},
							NoProxy:   "localhost",
						},
					},
					Tools: []AIGatewayMCPPassthroughTool{{
						Name:   "create_issue",
						Access: AIGatewayMCPToolAccess{Acls: AIGatewayMCPACLs{Deny: []AIGatewayMCPACLRef{{Name: "ai-gw-denied-consumer-group"}}}},
					}},
				},
			}),
			objects: []runtime.Object{newReferencedPolicy(), newReferencedConsumerGroup(), newReferencedDeniedConsumerGroup(), newReferencedAuthStrategy()},
			want: `
type: passthrough-listener
name: github-mcp
display_name: GitHub MCP
enabled: false
upstream_url: https://api.githubcopilot.com/mcp
policies: [aigw-policy]
config:
  route:
    paths: [/github-mcp]
  proxy:
    http_proxy: {host: proxy.local, port: 3128}
    no_proxy: localhost
tools:
- name: create_issue
  access:
    acls: {deny: [denied-users]}
access:
  acl_attribute_type: oauth_access_token
  access_token_claim_field: .user.email
  acls: {deny: [denied-users]}
  auth_strategies: [key-auth-1]
  metadata:
    resource: https://mcp.example.com/github
    endpoint: /.well-known/oauth-protected-resource
    discovery_endpoint: /.well-known/oauth-authorization-server
    authorization_servers: [https://auth.example.com]
    scopes_supported: [repo]
`,
		},
		{
			name: "upstream-server: tools with schemas, tools_cache_ttl_seconds",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeUpstreamServer,
				UpstreamServer: &AIGatewayMCPServerUpstreamServer{
					Name:        "flights-upstream",
					DisplayName: "Flights Upstream MCP Server",
					Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					Config: AIGatewayMCPServerUpstreamServerConfig{
						URL:                  "https://flights-api.example/mcp",
						Route:                AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/flights-upstream"}},
						ToolsCacheTtlSeconds: 30,
						Server: AIGatewayMCPServerUpstreamServerServerConfig{
							Timeout: 3000,
						},
					},
					Tools: []AIGatewayMCPUpstreamTool{{
						Name:        "search_flights",
						Description: "Search flights (upstream override)",
						InputSchema: apiextensionsv1.JSON{Raw: []byte(`{"type":"object"}`)},
					}},
				},
			}),
			objects: []runtime.Object{newReferencedPolicy()},
			want: `
type: upstream-server
name: flights-upstream
display_name: Flights Upstream MCP Server
upstream_url: https://flights-api.example/mcp
policies: [aigw-policy]
config:
  route:
    paths: [/mcp/flights-upstream]
  server:
    timeout: 3000
  tools_cache_ttl_seconds: 30
tools:
- name: search_flights
  description: Search flights (upstream override)
  input_schema: {"type": "object"}
`,
		},
		{
			name:    "no apiSpec",
			obj:     newMCPServer(nil),
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "variant missing for the selected type",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeConversionOnly,
			}),
			wantErr: "spec.apiSpec.conversion-only is required",
		},
		{
			name: "dangling policy reference",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeConversionOnly,
				ConversionOnly: &AIGatewayMCPServerConversionOnly{
					Name:     "flights",
					Policies: []AIGatewayPolicyRef{{Name: "does-not-exist"}},
					Config:   AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{URL: "http://upstream:8080"},
				},
			}),
			wantErr: "resolving AIGatewayMCPServer default/sample-ai-gw-mcp-server policies",
		},
		{
			name: "dangling source reference",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeListener,
				Listener: &AIGatewayMCPServerListener{
					Name:    "mcp-listener",
					Sources: []AIGatewayMCPServerRef{{Name: "does-not-exist"}},
					Config:  AIGatewayMCPServerListenerConfig{Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp"}}},
				},
			}),
			wantErr: "resolving AIGatewayMCPServer default/sample-ai-gw-mcp-server sources",
		},
		{
			name: "cross-namespace source reference rejected",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeListener,
				Listener: &AIGatewayMCPServerListener{
					Name:    "mcp-listener",
					Sources: []AIGatewayMCPServerRef{{Name: "ai-gw-source-mcp-server", Namespace: "other-namespace"}},
					Config:  AIGatewayMCPServerListenerConfig{Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp"}}},
				},
			}),
			objects: []runtime.Object{newReferencedSourceMCPServer()},
			wantErr: "cross-namespace reference",
		},
		{
			name: "dangling access ACL consumer group reference",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeListener,
				Listener: &AIGatewayMCPServerListener{
					Name: "mcp-listener",
					Access: &AIGatewayMCPServerListenerAccess{
						AclAttributeType: AIGatewayMCPServerListenerAccessTypeConsumer,
						Consumer: &AIGatewayMCPServerListenerConsumer{
							Acls: AIGatewayMCPACLs{Allow: []AIGatewayMCPACLRef{{Name: "does-not-exist"}}},
						},
					},
					Config: AIGatewayMCPServerListenerConfig{Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp"}}},
				},
			}),
			wantErr: "access.acls.allow",
		},
		{
			name: "dangling tool ACL consumer group reference",
			obj: newMCPServer(&AIGatewayMCPServerConfig{
				Type: AIGatewayMCPServerConfigTypeConversionOnly,
				ConversionOnly: &AIGatewayMCPServerConversionOnly{
					Name:   "flights",
					Config: AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{URL: "http://upstream:8080"},
					Tools: []AIGatewayMCPConversionTool{{
						Name:   "search_flights",
						Access: AIGatewayMCPToolAccess{Acls: AIGatewayMCPACLs{Allow: []AIGatewayMCPACLRef{{Name: "does-not-exist"}}}},
					}},
				},
			}),
			wantErr: "tools[0].access.acls.allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWMCPServer(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, wantMCPServerFromYAML(t, tt.want), got)
		})
	}
}

// TestAIGatewayMCPServer_ToAIGWMCPServer_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWMCPServerPayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.MCPServer doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale. The conversion-listener variant is
// the widest one (every config block except the upstream-server-only tools cache), so it is
// the one exercised here.
func TestAIGatewayMCPServer_ToAIGWMCPServer_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	obj := &AIGatewayMCPServer{
		Name: "sample-ai-gw-mcp-server", Namespace: "default",
		Spec: AIGatewayMCPServerSpec{
			APISpec: AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &AIGatewayMCPServerConfig{
					Type: AIGatewayMCPServerConfigTypeConversionListener,
					ConversionListener: &AIGatewayMCPServerConversionListener{
						Name:        "flights-listener",
						DisplayName: "Flights MCP Listener",
						// "Disabled" half of the enum: the "Enabled" -> true case is covered by
						// the table test.
						Enabled: "Disabled",
						Labels:  PublicLabels{"app": "test1"},
						// Pinned by KnownFields(true) below: an un-dropped managed_by fails the
						// strict decode.
						ManagedBy: ManagedBy{"kong-operator": "true"},
						// Policies, access refs and tool ACLs are stripped by the payload
						// builder (they are re-attached resolved by ToAIGWMCPServer), so the
						// strict decode must not see them.
						Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
						Access: &AIGatewayMCPServerConversionListenerAccess{
							AclAttributeType: AIGatewayMCPServerConversionListenerAccessTypeConsumer,
							Consumer: &AIGatewayMCPServerListenerConsumer{
								Acls:              AIGatewayMCPACLs{Allow: []AIGatewayMCPACLRef{{Name: "ai-gw-consumer-group"}}},
								AuthStrategies:    []AIGatewayAuthStrategyRef{{Name: "ai-gw-auth-strategy"}},
								IdentityProviders: []AIGatewayIdentityProviderReference{"legacy-idp"},
							},
						},
						Config: AIGatewayMCPServerConversionListenerConfig{
							URL:   "http://upstream:8080",
							Route: AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/flights"}, StripPath: "Disabled"},
							Logging: AIGatewayMCPServerConversionListenerConfigLogging{
								Audits: "Enabled",
							},
							MaxRequestBodySize: 1024,
							Server: AIGatewayMCPServerServerConfigBase{
								ForwardClientHeaders: "Disabled",
								Session: AIGatewayMCPServerServerConfigBaseSession{
									Managed: "Disabled",
								},
							},
							// Both have no aigw slot: pinned dropped by the strict decode.
							AllowedVersions: []string{"2025-06-18"},
							Cache: AIGatewayMCPServerConversionListenerConfigCache{
								Discover: AIGatewayMCPServerCacheHint{CacheScope: "private", TtlMs: 1000},
							},
							Upstream: AIGatewayUpstreamConfig{
								Auth: &AIGatewayUpstreamConfigAuth{
									Type:                     AIGatewayUpstreamConfigAuthTypeAIGatewayUpstreamAuthAWS,
									AIGatewayUpstreamAuthAWS: &AIGatewayUpstreamAuthAWS{Region: "us-east-1"},
								},
							},
						},
						Tools: []AIGatewayMCPConversionTool{{
							Name:        "get_flight_status",
							Description: "Get flight status",
							Method:      "GET",
							Path:        "/flights/{flightNumber}/status",
							Annotations: AIGatewayMCPToolAnnotations{ReadOnlyHint: "Enabled"},
						}},
					},
				},
			},
		},
	}
	data, err := obj.marshalAIGWMCPServerPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var mcp aigw.MCPServer
	require.NoError(t, dec.Decode(&mcp))

	require.Equal(t, "flights-listener", mcp.Name)
	require.Equal(t, "Flights MCP Listener", mcp.DisplayName)
	require.Equal(t, "conversion-listener", mcp.Type)
	require.NotNil(t, mcp.Enabled)
	require.False(t, *mcp.Enabled)
	require.Equal(t, aigw.Labels{"app": "test1"}, mcp.Labels)
	require.Nil(t, mcp.Policies)
	require.Equal(t, "http://upstream:8080", mcp.UpstreamURL)
	require.NotNil(t, mcp.Config.Logging)
	require.NotNil(t, mcp.Config.Logging.Audits)
	require.True(t, *mcp.Config.Logging.Audits)
	require.NotNil(t, mcp.Config.Route.StripPath)
	require.False(t, *mcp.Config.Route.StripPath)
	require.NotNil(t, mcp.Config.Upstream)
	require.NotNil(t, mcp.Config.Upstream.Auth)
	require.Equal(t, "aws", mcp.Config.Upstream.Auth.Type)
	require.Equal(t, "us-east-1", mcp.Config.Upstream.Auth.Region)
	require.Equal(t, "legacy-idp", mcp.Access.AuthStrategies[0])
	require.Len(t, mcp.Tools, 1)
	require.NotNil(t, mcp.Tools[0].Annotations["read_only_hint"])
	require.True(t, mcp.Tools[0].Annotations["read_only_hint"].(bool))
}
