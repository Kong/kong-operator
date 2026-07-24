package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		ObjectMeta: metav1.ObjectMeta{Name: "sample-ai-gw-model", Namespace: "default"},
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
								Model: &AIGatewayModelRouteConfigModel{
									Type:        AIGatewayModelRouteConfigModelTypePathAliases,
									PathAliases: []string{"gpt-4o-mini"},
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
	provider := &AIGatewayModelProvider{ObjectMeta: metav1.ObjectMeta{Name: "ai-gw-provider-openai", Namespace: "default"}}
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
	require.JSONEq(t, `{"path_aliases":["gpt-4o-mini"]}`, string(decoded.Config.Route.Model))
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
		ObjectMeta: metav1.ObjectMeta{Name: "sample-ai-gw-model", Namespace: "default"},
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
								Model: &AIGatewayModelRouteConfigModel{
									Type:    AIGatewayModelRouteConfigModelTypeHeaders,
									Headers: &apiextensionsv1.JSON{Raw: []byte(`{"X-Kong-LLM-Model":["gpt-4o-mini-model"]}`)},
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
	provider := &AIGatewayModelProvider{ObjectMeta: metav1.ObjectMeta{Name: "ai-gw-provider-openai", Namespace: "default"}}
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
	require.JSONEq(t, `{"headers":{"X-Kong-LLM-Model":["gpt-4o-mini-model"]}}`, string(decoded.Config.Route.Model))
}
