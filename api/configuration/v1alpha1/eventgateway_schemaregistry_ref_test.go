package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventGatewaySchemaRegistryRefUnmarshalJSON(t *testing.T) {
	for name, tc := range map[string]struct {
		data string
		want EventGatewaySchemaRegistryRef
	}{
		"a reference to the object": {
			data: `{"kind":"EventGatewaySchemaRegistry","name":"orders","namespace":"other"}`,
			want: EventGatewaySchemaRegistryRef{Kind: "EventGatewaySchemaRegistry", Name: "orders", Namespace: "other"},
		},
		// A stored reference by Konnect name, as the API server serves it once
		// pruned by the current schema.
		"a pruned reference by Konnect name": {
			data: `{"kind":"EventGatewaySchemaRegistry","name":{}}`,
			want: EventGatewaySchemaRegistryRef{Kind: "EventGatewaySchemaRegistry"},
		},
		"a reference by Konnect name, not pruned": {
			data: `{"type":"name","name":{"name":"konnect-registry"}}`,
			want: EventGatewaySchemaRegistryRef{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var got EventGatewaySchemaRegistryRef
			require.NoError(t, json.Unmarshal([]byte(tc.data), &got))
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("a policy holding a pruned reference decodes", func(t *testing.T) {
		var policy EventGatewayVirtualClusterProducePolicy
		require.NoError(t, json.Unmarshal([]byte(`{"spec":{"apiSpec":{"type":"schemaValidation","schemaValidation":{"name":"p",`+
			`"config":{"type":"confluentSchemaRegistry","confluentSchemaRegistry":{"valueValidationAction":"reject","schemaRegistry":{"kind":"EventGatewaySchemaRegistry","name":{}}}}}}}}`), &policy))
		_, err := resolveEventGatewayVirtualClusterProducePolicySchemaValidationConfigConfluentSchemaRegistrySchemaRegistry(t.Context(), nil, &policy)
		require.ErrorContains(t, err, "EventGatewaySchemaRegistry reference has no name set")
	})
}

// TestRequiredReferenceWithoutNameFails verifies a required reference without
// a name, e.g. a stored reference by Konnect ID once pruned, fails resolution
// instead of being left in the payload sent to Konnect.
func TestRequiredReferenceWithoutNameFails(t *testing.T) {
	for name, tc := range map[string]struct {
		config *EventGatewayVirtualClusterProducePolicyConfig
		want   string
	}{
		"encrypt static key": {
			config: &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy,
				EncryptPolicy: &EventGatewayEncryptPolicy{Config: EventGatewayEncryptConfig{
					EncryptionKey: &EventGatewayEncryptConfigEncryptionKey{
						Type:   EventGatewayEncryptConfigEncryptionKeyTypeStatic,
						Static: &EncryptionKeyStatic{Key: EventGatewayStaticKeyRef{Kind: "EventGatewayStaticKey"}},
					},
				}},
			},
			want: "EventGatewayStaticKey reference has no name set",
		},
		"transcode schema registry": {
			config: &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate,
				ParsedRecordTranscodeProducePolicyCreate: &EventGatewayParsedRecordTranscodeProducePolicyCreate{
					Config: &EventGatewayParsedRecordTranscodeProducePolicyCreateConfig{
						OutputFormat: EventGatewayParsedRecordTranscodeProducePolicyCreateConfigTypeEventGatewayParsedRecordTranscodeProduceConfigAvro,
						EventGatewayParsedRecordTranscodeProduceConfigAvro: &EventGatewayParsedRecordTranscodeProduceConfigAvro{
							SchemaSource: &EventGatewayParsedRecordTranscodeProduceConfigAvroSchemaSource{
								Type: EventGatewayParsedRecordTranscodeProduceConfigAvroSchemaSourceTypeReferenc,
								Referenc: &EventGatewayParsedRecordTranscodeSchemaSourceReference{
									SchemaRegistry: EventGatewaySchemaRegistryRef{Kind: "EventGatewaySchemaRegistry"},
								},
							},
						},
					},
				},
			},
			want: "EventGatewaySchemaRegistry reference has no name set",
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := &EventGatewayVirtualClusterProducePolicy{
				Spec: EventGatewayVirtualClusterProducePolicySpec{
					APISpec: EventGatewayVirtualClusterProducePolicyAPISpec{EventGatewayVirtualClusterProducePolicyConfig: tc.config},
				},
			}
			policy.Namespace = "default"
			require.ErrorContains(t, policy.ResolveKonnectReferences(t.Context(), nil), tc.want)
		})
	}
}
