package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newTestDatastore returns an AIGatewayDatastore named "my-datastore" whose
// Konnect name differs from its Kubernetes name, so tests observe which one is
// sent.
func newTestDatastore(namespace, konnectID string) *AIGatewayDatastore {
	ds := &AIGatewayDatastore{
		Name: "my-datastore", Namespace: namespace,
		Spec: AIGatewayDatastoreSpec{
			APISpec: AIGatewayDatastoreAPISpec{
				AIGatewayDatastoreConfig: &AIGatewayDatastoreConfig{
					Type: AIGatewayDatastoreConfigTypeRedisCE,
					RedisCE: &RedisCEDatastore{
						Name: "shared-redis",
						Config: RedisCEDatastoreConfig{
							Host: "redis.example.com",
							Port: 6379,
						},
					},
				},
			},
		},
	}
	ds.SetGatewayID("gateway-1")
	ds.SetKonnectID(konnectID)
	return ds
}

func newDatastoreRefTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

// TestAIGatewayPolicy_DatastoreRef verifies datastore references resolve to the
// referenced CR's Konnect name and keep the OAS item shape ([{name: ...}]).
func TestAIGatewayPolicy_DatastoreRef(t *testing.T) {
	newPolicy := func() *AIGatewayPolicy {
		p := &AIGatewayPolicy{
			Name: "policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name:        "rate-limiting",
					DisplayName: "Rate limiting",
					Type:        "rate-limiting-advanced",
					Datastores:  []AIGatewayDatastoreNamespacedRef{{Name: "my-datastore"}},
				},
			},
		}
		p.SetGatewayID("gateway-1")
		return p
	}

	t.Run("resolved datastore's Konnect name is sent", func(t *testing.T) {
		p := newPolicy()
		cl := newDatastoreRefTestClient(t, newTestDatastore("default", "datastore-konnect-id"))

		create, err := p.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		require.Len(t, create.Datastores, 1)
		assert.Equal(t, "shared-redis", create.Datastores[0].Name)

		// The SDK item keeps its OAS shape: one object per datastore.
		data, err := json.Marshal(create.Datastores)
		require.NoError(t, err)
		assert.JSONEq(t, `[{"name":"shared-redis"}]`, string(data))
	})

	t.Run("a datastore in another namespace is rejected", func(t *testing.T) {
		p := newPolicy()
		p.Spec.APISpec.Datastores = []AIGatewayDatastoreNamespacedRef{{Namespace: "other", Name: "my-datastore"}}
		cl := newDatastoreRefTestClient(t, newTestDatastore("other", "datastore-konnect-id"))

		_, err := p.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		require.Error(t, err)
	})

	t.Run("a datastore not yet programmed in Konnect is reported", func(t *testing.T) {
		p := newPolicy()
		cl := newDatastoreRefTestClient(t, newTestDatastore("default", ""))

		_, err := p.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		var notProgrammed ReferenceNotProgrammedError
		require.ErrorAs(t, err, &notProgrammed)
		assert.Equal(t, "AIGatewayDatastore", notProgrammed.Kind)
	})
}

// TestAIGatewayModel_DatastoreRef verifies the nested reference path (a datastore
// inside the model's selected union variant) resolves and keeps the item shape.
func TestAIGatewayModel_DatastoreRef(t *testing.T) {
	model := &AIGatewayModel{
		Name: "model", Namespace: "default",
		Spec: AIGatewayModelSpec{
			APISpec: AIGatewayModelAPISpec{
				AIGatewayModelConfig: &AIGatewayModelConfig{
					Type: AIGatewayModelConfigTypeAPI,
					API: &AIGatewayModelAPI{
						Name:         "gpt",
						DisplayName:  "GPT",
						Enabled:      "Enabled",
						Capabilities: []string{"generate"},
						Formats:      []AIGatewayModelFormat{{Type: "openai"}},
						Config: AIGatewayModelAPIConfig{
							Route: AIGatewayModelRouteConfig{
								Paths: []string{"/v1/chat/completions"},
								Model: AIGatewayModelSelectorConfig{PathParam: "model", Values: []string{"gpt"}},
							},
						},
						Targets: []AIGatewayTarget{{
							Name:     "target",
							Provider: AIGatewayModelProviderRef{Name: "provider"},
							Config: &AIGatewayTargetConfig{
								Type:   AIGatewayTargetConfigTypeOpenai,
								Openai: &AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1/chat/completions"},
							},
						}},
						Datastores: []AIGatewayDatastoreNamespacedRef{{Name: "my-datastore"}},
					},
				},
			},
		},
	}
	model.SetGatewayID("gateway-1")
	// The model's targets also reference a provider, which is resolved alongside
	// the datastores.
	provider := &AIGatewayModelProvider{
		Name: "provider", Namespace: "default",
		Spec: AIGatewayModelProviderSpec{
			APISpec: AIGatewayModelProviderAPISpec{
				AIGatewayModelProviderConfig: &AIGatewayModelProviderConfig{
					Type:   AIGatewayModelProviderConfigTypeOpenai,
					Openai: &AIGatewayModelProviderOpenai{Name: "openai-provider"},
				},
			},
		},
	}
	provider.SetGatewayID("gateway-1")
	provider.SetKonnectID("provider-id")
	cl := newDatastoreRefTestClient(t,
		newTestDatastore("default", "datastore-konnect-id"),
		provider,
	)

	create, err := model.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)
	require.Len(t, create.AIGatewayModelAPI.Datastores, 1)
	assert.Equal(t, "shared-redis", create.AIGatewayModelAPI.Datastores[0].Name)
}
