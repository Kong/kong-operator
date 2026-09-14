package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// aigwFixtureReferences are the referenced entities shared by both test cases below, shaped
// after config/samples/konnect_aigatewaymodel.yaml. Deliberately no SetKonnectID call anywhere:
// that is the assertion that ToAIGWModel's ref resolution does not require Konnect programming,
// unlike the generated resolve* helpers (contrast aigatewaymodel_manual_test.go:66, which has
// to set one).
func aigwFixtureReferences() []client.Object {
	return []client.Object{
		&AIGatewayModelProvider{
			Name: "ai-gw-provider-openai", Namespace: "default",
			Spec: AIGatewayModelProviderSpec{
				APISpec: AIGatewayModelProviderAPISpec{
					AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
						Type: AIGatewayModelProviderConfigTypeOpenai,
						Openai: &AIGatewayModelProviderOpenai{
							Name:        "openai-provider",
							DisplayName: "OpenAI",
						},
					},
				},
			},
		},
		&AIGatewayAuthStrategy{
			Name: "ai-gw-model-auth-strategy", Namespace: "default",
			Spec: AIGatewayAuthStrategySpec{
				APISpec: AIGatewayAuthStrategyAPISpec{
					AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
						Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
						KeyAuth: &AIGatewayAuthStrategyKeyAuth{
							Name:        "model-key-auth-provider",
							DisplayName: "Model Key Auth",
						},
					},
				},
			},
		},
		&AIGatewayConsumerGroup{
			Name: "ai-gw-model-consumer-group", Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{
					Name:        "model-dev-users",
					DisplayName: "Dev Users Group",
				},
			},
		},
		&AIGatewayPolicy{
			Name: "ai-gw-policy-prompt-decorator", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name:        "aigw-policy-prompt-decorator",
					DisplayName: "AIGateway Policy Prompt Decorator",
				},
			},
		},
	}
}

// TestAIGatewayModel_ToAIGWModel covers both union variants (type: api, type: model), each with
// the full reference set (policy, target provider, auth strategy, ACL), resolved without any of
// the referenced objects carrying a Konnect ID.
func TestAIGatewayModel_ToAIGWModel(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	// aigw/doc.go doesn't alias ModelAccess or ModelPathSelectorConfig, so the two fields of
	// those types are filled in by assignment below rather than in the composite literal.
	wantAPI := &aigw.Model{
		Type:         "api",
		Name:         "gpt-4o-mini",
		DisplayName:  "GPT-4o Mini",
		Enabled:      new(true),
		Formats:      []aigw.Format{{Type: "openai"}},
		Capabilities: []string{"files"},
		Labels:       aigw.Labels{"app": "test1", "env": "test"},
		Policies:     []string{"aigw-policy-prompt-decorator"},
		Config: aigw.ModelConfig{
			Route: aigw.ModelRouteConfig{
				Paths: []string{"/v1/chat/completions"},
				Model: aigw.ModelAliasConfig{Values: []string{"gpt-4o-mini"}},
			},
		},
		TargetModels: []aigw.TargetModel{
			{
				Name:     "gpt-4o-mini",
				Provider: "openai-provider",
				Config: aigw.TargetModelConfig{
					Type:    "openai",
					Options: map[string]any{"upstream_url": "https://api.openai.com/v1/chat/completions"},
				},
			},
		},
	}
	wantAPI.Access.AuthStrategies = []string{"model-key-auth-provider"}
	wantAPI.Access.ACLs = aigw.ACLs{Allow: []string{"model-dev-users"}}
	wantAPI.Config.Route.Model.Path.PathParam = "model"

	wantModel := &aigw.Model{
		Type:         "model",
		Name:         "gpt-4o-mini-model",
		DisplayName:  "GPT-4o Mini",
		Enabled:      new(true),
		Formats:      []aigw.Format{{Type: "openai"}},
		Capabilities: []string{"generate"},
		Policies:     []string{"aigw-policy-prompt-decorator"},
		Config: aigw.ModelConfig{
			Route: aigw.ModelRouteConfig{
				Paths: []string{"/v1/chat/completions"},
			},
		},
		TargetModels: []aigw.TargetModel{
			{
				Name:     "gpt-4o-mini-model",
				Provider: "openai-provider",
				Config: aigw.TargetModelConfig{
					Type:    "openai",
					Options: map[string]any{"upstream_url": "https://api.openai.com/v1/chat/completions"},
				},
			},
		},
	}
	wantModel.Access.AuthStrategies = []string{"model-key-auth-provider"}
	wantModel.Access.ACLs = aigw.ACLs{Deny: []string{"model-dev-users"}}

	tests := []struct {
		name    string
		obj     *AIGatewayModel
		want    *aigw.Model
		wantErr string
	}{
		{
			name: "type api, full references, model selector re-nested",
			obj: &AIGatewayModel{
				Name: "sample-ai-gw-model-api", Namespace: "default",
				Spec: AIGatewayModelSpec{
					APISpec: AIGatewayModelAPISpec{
						AIGatewayModelConfig: &AIGatewayModelConfig{
							Type: AIGatewayModelConfigTypeAPI,
							API: &AIGatewayModelAPI{
								Name:         "gpt-4o-mini",
								DisplayName:  "GPT-4o Mini",
								Enabled:      "Enabled",
								Formats:      []AIGatewayModelFormat{{Type: "openai"}},
								Capabilities: []string{"files"},
								Labels:       PublicLabels{"app": "test1", "env": "test"},
								Access: AIGatewayModelAccess{
									AuthStrategies: []AIGatewayAuthStrategyRef{{Name: "ai-gw-model-auth-strategy"}},
									Acls: &AIGatewayModelAccessAcls{
										Type: AIGatewayModelAccessAclsTypeAllow,
										Allow: &AIGatewayAllowACL{
											Allow: []AIGatewayACLRef{{Name: "ai-gw-model-consumer-group"}},
										},
									},
								},
								Config: AIGatewayModelAPIConfig{
									Route: AIGatewayModelRouteConfig{
										Paths: []string{"/v1/chat/completions"},
										Model: AIGatewayModelSelectorConfig{
											PathParam: "model",
											Values:    []string{"gpt-4o-mini"},
										},
									},
								},
								Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy-prompt-decorator"}},
								Targets: []AIGatewayTarget{
									{
										Name:     "gpt-4o-mini",
										Provider: AIGatewayModelProviderRef{Name: "ai-gw-provider-openai"},
										Config: &AIGatewayTargetConfig{
											Type:   AIGatewayTargetConfigTypeOpenai,
											Openai: &AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1/chat/completions"},
										},
									},
								},
							},
						},
					},
				},
			},
			want: wantAPI,
		},
		{
			name: "type model, full references incl. deny ACL",
			obj: &AIGatewayModel{
				Name: "sample-ai-gw-model", Namespace: "default",
				Spec: AIGatewayModelSpec{
					APISpec: AIGatewayModelAPISpec{
						AIGatewayModelConfig: &AIGatewayModelConfig{
							Type: AIGatewayModelConfigTypeModel,
							Model: &AIGatewayModelModel{
								Name:         "gpt-4o-mini-model",
								DisplayName:  "GPT-4o Mini",
								Enabled:      "Enabled",
								Formats:      []AIGatewayModelFormat{{Type: "openai"}},
								Capabilities: []string{"generate"},
								Access: AIGatewayModelAccess{
									AuthStrategies: []AIGatewayAuthStrategyRef{{Name: "ai-gw-model-auth-strategy"}},
									// The "model" variant's ACLs have no generated resolver
									// (only the "api" variant does) - this pins that the
									// hand-written resolver covers both.
									Acls: &AIGatewayModelAccessAcls{
										Type: AIGatewayModelAccessAclsTypeDeny,
										Deny: &AIGatewayDenyACL{
											Deny: []AIGatewayACLRef{{Name: "ai-gw-model-consumer-group"}},
										},
									},
								},
								Config: AIGatewayModelModelConfig{
									Route: AIGatewayModelRouteConfig{
										Paths: []string{"/v1/chat/completions"},
									},
								},
								Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy-prompt-decorator"}},
								Targets: []AIGatewayTarget{
									{
										Name:     "gpt-4o-mini-model",
										Provider: AIGatewayModelProviderRef{Name: "ai-gw-provider-openai"},
										Config: &AIGatewayTargetConfig{
											Type:   AIGatewayTargetConfigTypeOpenai,
											Openai: &AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1/chat/completions"},
										},
									},
								},
							},
						},
					},
				},
			},
			want: wantModel,
		},
		{
			name: "missing referenced provider",
			obj: &AIGatewayModel{
				Name: "missing-provider", Namespace: "default",
				Spec: AIGatewayModelSpec{
					APISpec: AIGatewayModelAPISpec{
						AIGatewayModelConfig: &AIGatewayModelConfig{
							Type: AIGatewayModelConfigTypeModel,
							Model: &AIGatewayModelModel{
								Name:        "orphan-model",
								DisplayName: "Orphan",
								Formats:     []AIGatewayModelFormat{{Type: "openai"}},
								Config: AIGatewayModelModelConfig{
									Route: AIGatewayModelRouteConfig{Paths: []string{"/v1/x"}},
								},
								Targets: []AIGatewayTarget{
									{
										Name:     "orphan-target",
										Provider: AIGatewayModelProviderRef{Name: "does-not-exist"},
										Config:   &AIGatewayTargetConfig{Type: AIGatewayTargetConfigTypeOpenai},
									},
								},
							},
						},
					},
				},
			},
			wantErr: "targets[0].provider",
		},
		{
			name: "nil config",
			obj: &AIGatewayModel{
				Name: "no-config", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(aigwFixtureReferences()...).Build()
			got, err := tt.obj.ToAIGWModel(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayModel_ToAIGWModel_StrictRoundTrip guards against a dropped or renamed field: it
// decodes marshalAIGWPayload's output with yaml.v3's KnownFields(true), which errors on any key
// aigw.Model (and its non-custom-unmarshaled sub-types) doesn't recognize. It's this check that
// catches a regression of the config.route.model re-nesting (see renestModelSelector) - without
// the fix-up, that key round-trips as extra unknown fields instead of silently vanishing, only
// because a strict decoder is watching.
//
// KnownFields(true) is intentionally not used in production code: a CRD field ai-deck-converter
// doesn't know about yet would otherwise hard-fail every conversion at runtime instead of just
// this test.
func TestAIGatewayModel_ToAIGWModel_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayModelAPISpec{
		AIGatewayModelConfig: &AIGatewayModelConfig{
			Type: AIGatewayModelConfigTypeAPI,
			API: &AIGatewayModelAPI{
				Name:        "gpt-4o-mini",
				DisplayName: "GPT-4o Mini",
				Formats:     []AIGatewayModelFormat{{Type: "openai"}},
				Config: AIGatewayModelAPIConfig{
					Route: AIGatewayModelRouteConfig{
						Paths: []string{"/v1/chat/completions"},
						Model: AIGatewayModelSelectorConfig{
							PathParam: "model",
							Values:    []string{"gpt-4o-mini"},
						},
					},
				},
				Targets: []AIGatewayTarget{
					{
						Name:     "gpt-4o-mini",
						Provider: AIGatewayModelProviderRef{Name: "ai-gw-provider-openai"},
						Config: &AIGatewayTargetConfig{
							Type:   AIGatewayTargetConfigTypeOpenai,
							Openai: &AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1/chat/completions"},
						},
					},
				},
			},
		},
	}

	data, err := spec.marshalAIGWPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var model aigw.Model
	require.NoError(t, dec.Decode(&model))

	// Pin the fix itself, not just its absence of error.
	require.Equal(t, []string{"gpt-4o-mini"}, model.Config.Route.Model.Values)
	require.Equal(t, "model", model.Config.Route.Model.Path.PathParam)
}
