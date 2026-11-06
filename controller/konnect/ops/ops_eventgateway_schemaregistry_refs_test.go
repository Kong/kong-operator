package ops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestPolicySchemaRegistryReferences(t *testing.T) {
	t.Parallel()

	registry := func(name, gatewayID, konnectID string) *configurationv1alpha1.EventGatewaySchemaRegistry {
		var r configurationv1alpha1.EventGatewaySchemaRegistry
		require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: `+name+`, namespace: default}
spec:
  apiSpec:
    type: confluent
    confluent:
      name: `+name+`-in-konnect
      config: {endpoint: "https://registry.example.com", schemaType: avro}
`), &r))
		r.SetGatewayID(gatewayID)
		r.SetKonnectID(konnectID)
		return &r
	}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(
		registry("orders", "gateway-1", "orders-id"),
		registry("elsewhere", "gateway-2", "elsewhere-id"),
	).Build()

	producePolicy := func(t *testing.T, apiSpec string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		t.Helper()
		var p configurationv1alpha1.EventGatewayVirtualClusterProducePolicy
		require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: policy, namespace: default}
spec:
  apiSpec:
`+apiSpec), &p))
		p.SetGatewayID("gateway-1")
		return &p
	}
	validateWith := func(t *testing.T, schemaRegistry string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		return producePolicy(t, `
    type: schemaValidation
    schemaValidation:
      name: validate
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: `+schemaRegistry+`, valueValidationAction: reject}}
`)
	}
	schemaRegistrySent := func(t *testing.T, policy *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) any {
		t.Helper()
		req, err := policy.ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		data, err := json.Marshal(req.EventGatewayProducePolicyCreate)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(data, &body))
		return body["config"].(map[string]any)["schema_registry"]
	}

	t.Run("a schema validation names its registry by object", func(t *testing.T) {
		t.Parallel()

		policy := validateWith(t, `{type: namespacedRef, namespacedRef: {name: orders}}`)
		assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "orders"}},
			configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(policy))
		assert.Equal(t, map[string]any{"name": "orders-in-konnect"}, schemaRegistrySent(t, policy),
			"the registry is sent by its Konnect name")
	})

	t.Run("a reference by name is sent as it is", func(t *testing.T) {
		t.Parallel()

		policy := validateWith(t, `{type: name, name: {name: konnect-registry}}`)
		assert.Empty(t, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(policy))
		assert.Equal(t, map[string]any{"name": "konnect-registry"}, schemaRegistrySent(t, policy))
	})

	t.Run("a reference by id is sent as it is", func(t *testing.T) {
		t.Parallel()

		policy := validateWith(t, `{type: id, id: {id: 6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60}}`)
		assert.Empty(t, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(policy))
		assert.Equal(t, map[string]any{"id": "6f1d3c9a-2b7e-4c5d-8a1f-9e3b7c2d4f60"}, schemaRegistrySent(t, policy))
	})

	t.Run("a transcode names its registry by object", func(t *testing.T) {
		t.Parallel()

		// A transcode policy requires a parent_policy_id, so a full SDK
		// request isn't built: the references are collected and resolved.
		reference := `{type: reference, reference: {subject: orders-value, version: latest, schemaRefDestination: {type: none, none: {}}, schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}}}`
		produce := producePolicy(t, `
    type: transcode
    transcode:
      name: transcode
      config: {outputFormat: avro, avro: {failureMode: reject, schemaSource: `+reference+`}}
`)
		assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "orders"}},
			configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewaySchemaRegistry(produce))
		require.NoError(t, produce.ResolveKonnectReferences(t.Context(), cl))

		var consume configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
		require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: policy, namespace: default}
spec:
  apiSpec:
    type: transcode
    transcode:
      name: transcode
      config: {outputFormat: json, json: {failureMode: error, schemaSource: `+reference+`}}
`), &consume))
		consume.SetGatewayID("gateway-1")
		assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "orders"}},
			configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewaySchemaRegistry(&consume))
		require.NoError(t, consume.ResolveKonnectReferences(t.Context(), cl))
	})

	t.Run("a consume schema validation names its registry in either form", func(t *testing.T) {
		t.Parallel()

		for name, schemaRegistry := range map[string]string{
			"its name set directly": `{name: orders}`,
			"type namespacedRef":    `{type: namespacedRef, namespacedRef: {name: orders}}`,
		} {
			t.Run(name, func(t *testing.T) {
				var consume configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
				require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: policy, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: validate
      config: {type: confluentSchemaRegistry, confluentSchemaRegistry: {schemaRegistry: `+schemaRegistry+`, valueValidationAction: skip}}
`), &consume))
				consume.SetGatewayID("gateway-1")
				assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "orders"}},
					configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewaySchemaRegistry(&consume))
				req, err := consume.ToCreateEventGatewayVirtualClusterConsumePolicyRequest(t.Context(), cl)
				require.NoError(t, err)
				data, err := json.Marshal(req.EventGatewayConsumePolicyCreate)
				require.NoError(t, err)
				var body map[string]any
				require.NoError(t, json.Unmarshal(data, &body))
				assert.Equal(t, map[string]any{"name": "orders-in-konnect"}, body["config"].(map[string]any)["schema_registry"],
					"the registry is sent by its Konnect name")
			})
		}
	})

	t.Run("a registry that doesn't exist is reported", func(t *testing.T) {
		t.Parallel()

		_, err := validateWith(t, `{type: namespacedRef, namespacedRef: {name: missing}}`).
			ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
		var notFound configurationv1alpha1.ReferenceNotFoundError
		require.ErrorAs(t, err, &notFound)
	})

	t.Run("a registry of another gateway is reported", func(t *testing.T) {
		t.Parallel()

		_, err := validateWith(t, `{type: namespacedRef, namespacedRef: {name: elsewhere}}`).
			ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
		var otherGateway configurationv1alpha1.ReferenceDifferentGatewayError
		require.ErrorAs(t, err, &otherGateway)
	})
}

// A schema registry referenced by an inactive member of a consume policy's
// union, e.g. left over from a former config type, isn't one it uses.
func TestConsumePolicySchemaRegistryReferencesOfTheActiveMemberOnly(t *testing.T) {
	t.Parallel()

	var p configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy
	require.NoError(t, yaml.UnmarshalStrict([]byte(`
metadata: {name: policy, namespace: default}
spec:
  apiSpec:
    type: schemaValidation
    schemaValidation:
      name: validate
      config:
        type: json
        json: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: active}}, valueValidationAction: skip}
        confluentSchemaRegistry: {schemaRegistry: {type: namespacedRef, namespacedRef: {name: leftover}}, valueValidationAction: skip}
`), &p))
	assert.Equal(t, []client.ObjectKey{{Namespace: "default", Name: "active"}},
		configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewaySchemaRegistry(&p))
}
