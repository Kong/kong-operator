package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newAgentACLsFixture returns the allow-ACLs referencing the consumer group fixture below.
func newAgentACLsFixture() *AIGatewayAgentAccessAcls {
	return &AIGatewayAgentAccessAcls{
		Type:  AIGatewayAgentAccessAclsTypeAllow,
		Allow: &AIGatewayAllowACL{Allow: []AIGatewayACLRef{{Name: "ai-gw-consumer-group"}}},
	}
}

// newAgentDenyACLsFixture returns the deny-ACLs referencing the consumer group fixture below.
func newAgentDenyACLsFixture() *AIGatewayAgentAccessAcls {
	return &AIGatewayAgentAccessAcls{
		Type: AIGatewayAgentAccessAclsTypeDeny,
		Deny: &AIGatewayDenyACL{Deny: []AIGatewayACLRef{{Name: "ai-gw-consumer-group"}}},
	}
}

// TestAIGatewayAgent_ToAIGWAgent covers the policies, ACL consumer group and auth strategy
// reference resolution — deliberately without any SetKonnectID on the referenced entities,
// pinning that the on-prem translation uses the name-only resolver, unlike the generated
// Konnect resolver — plus the deprecated identity_providers folding
// into AuthStrategies and the Enabled/Disabled boolean normalization. (The managed_by drop
// is pinned by the strict round-trip test below, the only decode that can see the key.)
//
// aigw/doc.go doesn't alias AgentAccessConfig, AccessConfig or AgentConfig, so the want
// values set their fields through assignment on the zero values (same pattern as
// aigatewaymodel_aigw_manual_test.go).
func TestAIGatewayAgent_ToAIGWAgent(t *testing.T) {
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

	tests := []struct {
		name    string
		obj     *AIGatewayAgent
		objects []runtime.Object
		want    func() *aigw.Agent
		wantErr string
	}{
		{
			name: "policies, ACLs and auth strategies resolved by name, labels kept, managed_by dropped, identity_providers folded",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:        "sample-agent",
						DisplayName: "Sample Agent",
						Type:        "http",
						Enabled:     "Enabled",
						Labels:      PublicLabels{"app": "test1"},
						ManagedBy:   ManagedBy{"kong-operator": "true"},
						Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
						Access: AIGatewayAgentAccess{
							Acls:              newAgentACLsFixture(),
							AuthStrategies:    []AIGatewayAuthStrategyRef{{Name: "ai-gw-auth-strategy"}},
							IdentityProviders: []AIGatewayIdentityProviderReference{"legacy-idp"},
						},
						Config: AIGatewayAgentConfig{
							URL:                "http://upstream:8080",
							MaxRequestBodySize: 1024,
							Logging: AIGatewayAgentConfigLogging{
								Payloads:       "Enabled",
								MaxPayloadSize: 512,
							},
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedPolicy(), newReferencedConsumerGroup(), newReferencedAuthStrategy()},
			want: func() *aigw.Agent {
				agent := &aigw.Agent{
					Name:        "sample-agent",
					DisplayName: "Sample Agent",
					Type:        "http",
					Enabled:     new(true),
					Labels:      aigw.Labels{"app": "test1"},
					Policies:    []string{"aigw-policy"},
				}
				agent.Access.ACLs = aigw.ACLs{Allow: []string{"dev-users"}}
				// current-key authStrategies resolved first, then the folded deprecated
				// identity_providers.
				agent.Access.AuthStrategies = []string{"key-auth-1", "legacy-idp"}
				agent.Config.URL = "http://upstream:8080"
				agent.Config.MaxRequestBodySize = new(1024)
				agent.Config.Logging = &aigw.Logging{
					Payloads:       new(true),
					MaxPayloadSize: new(512),
				}
				return agent
			},
		},
		{
			name: "deny ACLs resolved",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-deny", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name: "deny-agent",
						Type: "a2a",
						Access: AIGatewayAgentAccess{
							Acls: newAgentDenyACLsFixture(),
						},
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			objects: []runtime.Object{newReferencedConsumerGroup()},
			want: func() *aigw.Agent {
				agent := &aigw.Agent{Name: "deny-agent", Type: "a2a"}
				agent.Access.ACLs = aigw.ACLs{Deny: []string{"dev-users"}}
				agent.Config.URL = "http://upstream:8080"
				return agent
			},
		},
		{
			name: "no refs",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-empty", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:   "empty-agent",
						Type:   "http",
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			want: func() *aigw.Agent {
				agent := &aigw.Agent{Name: "empty-agent", Type: "http"}
				agent.Config.URL = "http://upstream:8080"
				return agent
			},
		},
		{
			name: "dangling policy reference",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-dangling", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:     "dangling-agent",
						Type:     "http",
						Policies: []AIGatewayPolicyRef{{Name: "does-not-exist"}},
						Config:   AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			wantErr: "policies",
		},
		{
			name: "dangling ACL consumer group reference",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-dangling-acl", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name: "dangling-acl-agent",
						Type: "http",
						Access: AIGatewayAgentAccess{
							Acls: &AIGatewayAgentAccessAcls{
								Type:  AIGatewayAgentAccessAclsTypeAllow,
								Allow: &AIGatewayAllowACL{Allow: []AIGatewayACLRef{{Name: "does-not-exist"}}},
							},
						},
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			wantErr: "access.acls",
		},
		{
			name: "dangling auth strategy reference",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-dangling-auth", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name: "dangling-auth-agent",
						Type: "http",
						Access: AIGatewayAgentAccess{
							AuthStrategies: []AIGatewayAuthStrategyRef{{Name: "does-not-exist"}},
						},
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			wantErr: "access.authStrategies",
		},
		{
			name: "cross-namespace policy reference rejected",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-cross-ns", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name:     "cross-ns-agent",
						Type:     "http",
						Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy", Namespace: "other-namespace"}},
						Config:   AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			objects: []runtime.Object{newReferencedPolicy()},
			wantErr: "cross-namespace reference",
		},
		{
			name: "unsupported acls type",
			obj: &AIGatewayAgent{
				Name: "sample-ai-gw-agent-bad-acl", Namespace: "default",
				Spec: AIGatewayAgentSpec{
					APISpec: AIGatewayAgentAPISpec{
						Name: "bad-acl-agent",
						Type: "http",
						Access: AIGatewayAgentAccess{
							Acls: &AIGatewayAgentAccessAcls{Type: "bogus"},
						},
						Config: AIGatewayAgentConfig{URL: "http://upstream:8080"},
					},
				},
			},
			wantErr: "unsupported access.acls.type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWAgent(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want(), got)
		})
	}
}

// TestAIGatewayAgent_ToAIGWAgent_StrictRoundTrip guards against a dropped or renamed
// field: it decodes marshalAIGWAgentPayload's output with yaml.v3's KnownFields(true),
// which errors on any key aigw.Agent doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayAgent_ToAIGWAgent_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayAgentAPISpec{
		Name:        "sample-agent",
		DisplayName: "Sample Agent",
		Type:        "http",
		// "Disabled" half of the enum: the "Enabled" -> true case is covered by the table test.
		Enabled: "Disabled",
		Labels:  PublicLabels{"app": "test1"},
		// Pinned by KnownFields(true) below: an un-dropped managed_by fails the strict decode.
		ManagedBy: ManagedBy{"kong-operator": "true"},
		// Policies and access.acls/authStrategies are stripped by the payload builder (they
		// are re-attached resolved by ToAIGWAgent), so the strict decode must not see them.
		Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
		Access: AIGatewayAgentAccess{
			Acls:           newAgentACLsFixture(),
			AuthStrategies: []AIGatewayAuthStrategyRef{{Name: "ai-gw-auth-strategy"}},
			// identity_providers carries plain strings, so it stays in the payload and is
			// folded into AuthStrategies by aigw.AgentAccessConfig.UnmarshalYAML.
			IdentityProviders: []AIGatewayIdentityProviderReference{"legacy-idp"},
		},
		Config: AIGatewayAgentConfig{
			URL:                "http://upstream:8080",
			MaxRequestBodySize: 1024,
			Logging: AIGatewayAgentConfigLogging{
				Payloads:       "Disabled",
				MaxPayloadSize: 512,
			},
		},
	}
	data, err := spec.marshalAIGWAgentPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var agent aigw.Agent
	require.NoError(t, dec.Decode(&agent))

	require.Equal(t, "sample-agent", agent.Name)
	require.Equal(t, "Sample Agent", agent.DisplayName)
	require.Equal(t, "http", agent.Type)
	require.NotNil(t, agent.Enabled)
	require.False(t, *agent.Enabled)
	require.Equal(t, aigw.Labels{"app": "test1"}, agent.Labels)
	require.Nil(t, agent.Policies)
	require.Empty(t, agent.Access.ACLs)
	require.Equal(t, []string{"legacy-idp"}, agent.Access.AuthStrategies)
	require.Equal(t, "http://upstream:8080", agent.Config.URL)
	require.NotNil(t, agent.Config.Logging)
	require.NotNil(t, agent.Config.Logging.Payloads)
	require.False(t, *agent.Config.Logging.Payloads)
}
