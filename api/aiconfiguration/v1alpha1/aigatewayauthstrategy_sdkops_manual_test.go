package v1alpha1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAIGatewayAuthStrategyIdentityRealmsRequestJSON(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	newStrategy := func(realms []AIGWKeyAuthGeneratedConfigIdentityRealms) *AIGatewayAuthStrategy {
		return &AIGatewayAuthStrategy{
			Spec: AIGatewayAuthStrategySpec{
				APISpec: AIGatewayAuthStrategyAPISpec{
					AIGatewayAuthStrategyConfig: &AIGatewayAuthStrategyConfig{
						Type: AIGatewayAuthStrategyConfigTypeKeyAuth,
						KeyAuth: &AIGatewayAuthStrategyKeyAuth{
							Name:        "api-key",
							DisplayName: "API Key Authentication",
							Config: AIGWKeyAuthGeneratedConfig{
								KeyNames:       []string{"apikey"},
								IdentityRealms: realms,
								Principals: AIGWKeyAuthGeneratedConfigPrincipals{
									Enabled: "Enabled",
								},
							},
						},
					},
				},
			},
		}
	}

	verbs := map[string]func(*AIGatewayAuthStrategy) (any, error){
		"create": func(obj *AIGatewayAuthStrategy) (any, error) {
			return obj.ToCreateAIGatewayAuthStrategyRequest(context.Background(), cl)
		},
		"update": func(obj *AIGatewayAuthStrategy) (any, error) {
			return obj.ToUpdateAIGatewayAuthStrategyRequest(context.Background(), cl)
		},
	}

	tests := []struct {
		name    string
		realms  []AIGWKeyAuthGeneratedConfigIdentityRealms
		present bool
		length  int
	}{
		{name: "omitted"},
		{name: "empty", realms: []AIGWKeyAuthGeneratedConfigIdentityRealms{}, present: true},
		{
			name:    "populated",
			realms:  []AIGWKeyAuthGeneratedConfigIdentityRealms{{Scope: "cp"}},
			present: true,
			length:  1,
		},
	}

	for verb, toRequest := range verbs {
		for _, tt := range tests {
			t.Run(verb+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				req, err := toRequest(newStrategy(tt.realms))
				require.NoError(t, err)
				body, err := json.Marshal(req)
				require.NoError(t, err)

				var got map[string]any
				require.NoError(t, json.Unmarshal(body, &got))
				config, ok := got["config"].(map[string]any)
				require.True(t, ok, "request has no config object: %s", body)

				raw, present := config["identity_realms"]
				assert.Equal(t, tt.present, present, "request body: %s", body)
				if present {
					realms, ok := raw.([]any)
					require.True(t, ok, "identity_realms is not an array: %s", body)
					assert.Len(t, realms, tt.length)
				}
				principals, ok := config["principals"].(map[string]any)
				require.True(t, ok, "request has no principals object: %s", body)
				assert.Equal(t, true, principals["enabled"])
			})
		}
	}
}
