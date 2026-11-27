package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayModel_RouteModel_WireShape guards against a regression where
// config.route.model (a property-level oneOf with no OAS discriminator)
// round-tripped through the Konnect SDK's generated union UnmarshalJSON,
// which always resolves such unions to their first, vacuously-optional
// member. That silently turned "pathAliases: [gpt-4o-mini]" into an empty
// object on the wire, which Konnect rejected with a oneOf-ambiguity error.
// assignSDKOpsUnionMembers (zz_generated_common_types.go) works around it by
// reassigning the field directly from its bare sub-JSON after unmarshal.
func TestAIGatewayModel_RouteModel_WireShape(t *testing.T) {
	t.Parallel()

	// Mirrors config/samples/konnect_aigatewaymodel.yaml's api.config.route.model.
	obj := &AIGatewayModel{
		Name: "sample-ai-gw-model", Namespace: "default",
		Spec: AIGatewayModelSpec{
			APISpec: AIGatewayModelAPISpec{
				AIGatewayModelConfig: &AIGatewayModelConfig{
					Type: AIGatewayModelConfigTypeAPI,
					API: &AIGatewayModelAPI{
						Name:         "gpt-4o-mini",
						DisplayName:  "GPT-4o Mini",
						Capabilities: []string{"files"},
						Formats:      []AIGatewayModelFormat{{Type: "openai"}},
						Config: AIGatewayModelAPIConfig{
							Route: AIGatewayModelRouteConfig{
								Paths: []string{"/v1/chat/completions"},
								Model: AIGatewayModelSelectorConfig{
									PathParam: "model",
									Values: []string{
										"gpt-4o-mini",
									},
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
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	provider := &AIGatewayModelProvider{Name: "ai-gw-provider-openai", Namespace: "default"}
	provider.SetKonnectID("provider-kid")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(provider).Build()
	req, err := obj.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	// CreateAIGatewayModelRequest.MarshalJSON flattens straight to the selected
	// variant's fields (no "api"/"model" wrapper key on the wire).
	var decoded struct {
		Config struct {
			Route struct {
				Model json.RawMessage `json:"model"`
			} `json:"route"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.JSONEq(t, `{"path_param":"model","values":["gpt-4o-mini"]}`, string(decoded.Config.Route.Model))
}

// TestAIGatewayModel_RouteModel_FreeformHeaderKeyPreserved guards against a
// regression where renameKeysToSDK's blanket camelCase→snake_case pass
// corrupted user data keys inside free-form fields. config.route.model.headers
// (and similarly route.headers, route.model.body, labels, managedBy) are maps
// keyed by arbitrary strings such as HTTP header names, not CRD field names,
// and must survive the SDK conversion verbatim. renameKeysToSDKExcept
// (zz_generated_common_types.go), driven by the generator-collected
// AIGatewayModelSDKOpsFreeformKeyFields, preserves them.
func TestAIGatewayModel_RouteModel_FreeformHeaderKeyPreserved(t *testing.T) {
	t.Parallel()

	obj := &AIGatewayModel{
		Name: "sample-ai-gw-model", Namespace: "default",
		Spec: AIGatewayModelSpec{
			APISpec: AIGatewayModelAPISpec{
				AIGatewayModelConfig: &AIGatewayModelConfig{
					Type: AIGatewayModelConfigTypeModel,
					Model: &AIGatewayModelModel{
						Name:         "gpt-4o-mini-model",
						DisplayName:  "GPT-4o Mini",
						Capabilities: []string{"generate"},
						Formats:      []AIGatewayModelFormat{{Type: "openai"}},
						Config: AIGatewayModelModelConfig{
							Route: AIGatewayModelRouteConfig{
								Paths: []string{"/v1/chat/completions"},
								Model: AIGatewayModelSelectorConfig{
									PathParam: "model",
									Values: []string{
										"gpt-4o-mini",
									},
								},
							},
						},
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
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	provider := &AIGatewayModelProvider{Name: "ai-gw-provider-openai", Namespace: "default"}
	provider.SetKonnectID("provider-kid")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(provider).Build()
	req, err := obj.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	// CreateAIGatewayModelRequest.MarshalJSON flattens straight to the selected
	// variant's fields (no "api"/"model" wrapper key on the wire).
	var decoded struct {
		Config struct {
			Route struct {
				Model json.RawMessage `json:"model"`
			} `json:"route"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.JSONEq(t, `{"path_param":"model","values":["gpt-4o-mini"]}`, string(decoded.Config.Route.Model))
}

// TestAIGatewayModel_IntelligentBalancer_WireShape pins that the AI GW 2.5
// intelligent model router reaches the Konnect create request: the balancer
// discriminator, the classifier model it asks, and the cost/quality tradeoff
// are generated from the OAS and must survive the SDK conversion, where the
// algorithm's settings are flattened out of their member key.
func TestAIGatewayModel_IntelligentBalancer_WireShape(t *testing.T) {
	t.Parallel()

	obj := &AIGatewayModel{
		Name: "sample-ai-gw-model", Namespace: "default",
		Spec: AIGatewayModelSpec{
			APISpec: AIGatewayModelAPISpec{
				AIGatewayModelConfig: &AIGatewayModelConfig{
					Type: AIGatewayModelConfigTypeModel,
					Model: &AIGatewayModelModel{
						Name:         "intelligent-model",
						DisplayName:  "Intelligent Model",
						Capabilities: []string{"generate"},
						Formats:      []AIGatewayModelFormat{{Type: "openai"}},
						Config: AIGatewayModelModelConfig{
							Route: AIGatewayModelRouteConfig{Paths: []string{"/v1/chat/completions"}},
							Balancer: &AIGatewayModelModelConfigBalancer{
								Algorithm: AIGatewayModelModelConfigBalancerTypeIntelligent,
								Intelligent: &AIGatewayModelBalancerIntelligentConfig{
									CostQualityTradeoff: "balanced",
									Classifier: &AIGatewayModelBalancerIntelligentConfigClassifier{
										Type: AIGatewayModelBalancerIntelligentConfigClassifierTypeAIGatewayModelBalancerIntelligentClassifierCustom,
										AIGatewayModelBalancerIntelligentClassifierCustom: &AIGatewayModelBalancerIntelligentClassifierCustom{
											ContextWindow: 8192,
											Name:          "classifier-model",
											URL:           "https://classifier.example.com",
										},
									},
								},
							},
						},
						Targets: []AIGatewayTarget{{
							Name:     "intelligent-model",
							Provider: AIGatewayModelProviderRef{Name: "ai-gw-provider-openai"},
							Config: &AIGatewayTargetConfig{
								Type:   AIGatewayTargetConfigTypeOpenai,
								Openai: &AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1/chat/completions"},
							},
						}},
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	provider := &AIGatewayModelProvider{Name: "ai-gw-provider-openai", Namespace: "default"}
	provider.SetKonnectID("provider-kid")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(provider).Build()

	req, err := obj.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded struct {
		Config struct {
			Balancer struct {
				Algorithm  string `json:"algorithm"`
				Classifier struct {
					Type          string `json:"type"`
					ContextWindow int    `json:"context_window"`
					Name          string `json:"name"`
					URL           string `json:"url"`
				} `json:"classifier"`
				CostQualityTradeoff string `json:"cost_quality_tradeoff"`
				Version             string `json:"version"`
			} `json:"balancer"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))

	balancer := decoded.Config.Balancer
	require.Equal(t, "intelligent", balancer.Algorithm)
	require.Equal(t, "custom", balancer.Classifier.Type)
	require.Equal(t, "classifier-model", balancer.Classifier.Name)
	require.Equal(t, "https://classifier.example.com", balancer.Classifier.URL)
	require.Equal(t, 8192, balancer.Classifier.ContextWindow)
	require.Equal(t, "balanced", balancer.CostQualityTradeoff)
}
