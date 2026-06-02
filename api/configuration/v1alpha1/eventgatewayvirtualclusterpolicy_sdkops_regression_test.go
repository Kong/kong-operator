package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventGatewayVirtualClusterPolicyAPISpec_SelectedSDKOpsPayload_FlattensScalarAndArrayUnions(t *testing.T) {
	t.Parallel()

	staticNames := EventGatewayACLRuleResourceNamesStaticArray{
		{Match: "orders.*"},
		{Match: "invoices"},
	}
	dynamicNames := EventGatewayACLRuleResourceNamesDynamicArray("request.auth.claims.allowedConsumerGroups")

	spec := &EventGatewayVirtualClusterPolicyAPISpec{
		EventGatewayVirtualClusterPolicyConfig: &EventGatewayVirtualClusterPolicyConfig{
			Type: EventGatewayVirtualClusterPolicyConfigTypeEventGatewayACLsPolicy,
			EventGatewayACLsPolicy: &EventGatewayACLsPolicy{
				Name: "virtual-cluster-policy",
				Config: EventGatewayACLPolicyConfig{
					Rules: []EventGatewayACLRule{
						{
							Action:       "allow",
							ResourceType: "topic",
							Operations: []EventGatewayACLOperation{{
								Name: "read",
							}},
							ResourceNames: &EventGatewayACLRuleResourceNames{
								Type: EventGatewayACLRuleResourceNamesTypeStat,
								Stat: &staticNames,
							},
						},
						{
							Action:       "allow",
							ResourceType: "group",
							Operations: []EventGatewayACLOperation{{
								Name: "read",
							}},
							ResourceNames: &EventGatewayACLRuleResourceNames{
								Type:  EventGatewayACLRuleResourceNamesTypeDynam,
								Dynam: &dynamicNames,
							},
						},
					},
				},
			},
		},
	}

	payload, err := spec.marshalSDKOpsPayload()
	require.NoError(t, err)

	data, variant, err := spec.selectedSDKOpsPayload(payload)
	require.NoError(t, err)
	require.Equal(t, "EventGatewayACLsPolicy", variant)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))

	configPayload, ok := decoded["config"].(map[string]any)
	require.True(t, ok)

	rulesPayload, ok := configPayload["rules"].([]any)
	require.True(t, ok)
	require.Len(t, rulesPayload, 2)

	firstRule, ok := rulesPayload[0].(map[string]any)
	require.True(t, ok)
	staticPayload, ok := firstRule["resource_names"].([]any)
	require.True(t, ok)
	require.Len(t, staticPayload, 2)

	secondRule, ok := rulesPayload[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "request.auth.claims.allowedConsumerGroups", secondRule["resource_names"])

	createRequest, err := spec.ToCreateEventGatewayVirtualClusterClusterLevelPolicyRequest()
	require.NoError(t, err)
	require.NotNil(t, createRequest)

	updateRequest, err := spec.ToUpdateEventGatewayVirtualClusterClusterLevelPolicyRequest()
	require.NoError(t, err)
	require.NotNil(t, updateRequest)
}
