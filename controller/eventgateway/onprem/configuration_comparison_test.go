package onprem

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

func TestComparisonPreservesOnlyExplicitClusterPolicyOrder(t *testing.T) {
	var entities gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal([]byte(`{
		"virtual_clusters": [
			{"id":"00000000-0000-4000-8000-000000000001", "destination":{"id":"00000000-0000-4000-8000-000000000099"},
			 "cluster_policies":[
				{"id":"00000000-0000-4000-8000-000000000011","type":"acls"},
				{"id":"00000000-0000-4000-8000-000000000012","type":"acls"}],
			 "consume_policies":[
				{"id":"00000000-0000-4000-8000-000000000021","type":"skip_record"},
				{"id":"00000000-0000-4000-8000-000000000022","type":"skip_record"}],
			 "produce_policies":[
				{"id":"00000000-0000-4000-8000-000000000031","type":"modify_headers"},
				{"id":"00000000-0000-4000-8000-000000000032","type":"modify_headers"}]},
			{"id":"00000000-0000-4000-8000-000000000002", "destination":{"id":"00000000-0000-4000-8000-000000000099"},
			 "cluster_policies":[
				{"id":"00000000-0000-4000-8000-000000000041","type":"acls"},
				{"id":"00000000-0000-4000-8000-000000000042","type":"acls"}]}
		],
		"listeners":[{"id":"00000000-0000-4000-8000-000000000003","policies":[
			{"id":"00000000-0000-4000-8000-000000000051","type":"tls_server"},
			{"id":"00000000-0000-4000-8000-000000000052","type":"tls_server"}]}]
	}`), &entities))
	parentID := entities.VirtualClusters[0].Id
	encoded, err := json.Marshal(entities)
	require.NoError(t, err)
	var held gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal(encoded, &held))
	slices.Reverse(held.VirtualClusters[0].ConsumePolicies)
	slices.Reverse(held.VirtualClusters[0].ProducePolicies)
	slices.Reverse(held.VirtualClusters[1].ClusterPolicies)
	slices.Reverse(held.Listeners[0].Policies)
	slices.Reverse(held.VirtualClusters)
	key, err := comparisonKey(entities, parentID)
	require.NoError(t, err)
	same, err := sameEntities(held, key, parentID)
	require.NoError(t, err)
	require.True(t, same, "ordering remains insignificant outside the explicitly managed scope")

	slices.Reverse(held.VirtualClusters[1].ClusterPolicies)
	same, err = sameEntities(held, key, parentID)
	require.NoError(t, err)
	require.False(t, same, "remote reordering of the managed chain is drift")
	changedKey, err := comparisonKey(held, parentID)
	require.NoError(t, err)
	require.NotEqual(t, key, changedKey)

	defaultKey, err := comparisonKey(entities)
	require.NoError(t, err)
	same, err = sameEntities(held, defaultKey)
	require.NoError(t, err)
	require.True(t, same, "gateways without chains retain their existing comparison behavior")
	require.Equal(t, uuid.MustParse("00000000-0000-4000-8000-000000000011"),
		entities.VirtualClusters[0].ClusterPolicies[0].Id, "comparison must not mutate the desired document")
}

func TestComparisonAccountsForClusterPolicyDefaults(t *testing.T) {
	var pushed, held gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal([]byte(`{
		"virtual_clusters": [{
			"id":"00000000-0000-4000-8000-000000000001",
			"cluster_policies":[
				{"id":"00000000-0000-4000-8000-000000000012","type":"acls"},
				{"id":"00000000-0000-4000-8000-000000000011","type":"request_rule_validator",
				 "condition":"true","description":"configured","enabled":false}
			]
		}]
	}`), &pushed))
	require.NoError(t, json.Unmarshal([]byte(`{
		"virtual_clusters": [{
			"id":"00000000-0000-4000-8000-000000000001",
			"cluster_policies":[
				{"id":"00000000-0000-4000-8000-000000000012","type":"acls",
				 "condition":"","description":"","enabled":true},
				{"id":"00000000-0000-4000-8000-000000000011","type":"request_rule_validator",
				 "condition":"true","description":"configured","enabled":false}
			]
		}]
	}`), &held))
	original, err := json.Marshal(pushed)
	require.NoError(t, err)

	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "implicit order", true: "explicit chain"}[managed], func(t *testing.T) {
			var scopes []uuid.UUID
			if managed {
				scopes = []uuid.UUID{pushed.VirtualClusters[0].Id}
			}
			key, err := comparisonKey(withGWAdminDefaults(pushed), scopes...)
			require.NoError(t, err)
			same, err := sameEntities(held, key, scopes...)
			require.NoError(t, err)
			require.True(t, same, "backend defaults must not cause another push, and explicit values must be preserved")

			for field, value := range map[string]any{"condition": "false", "description": "changed", "enabled": false} {
				drifted := held
				drifted.VirtualClusters = slices.Clone(held.VirtualClusters)
				policies := slices.Clone(held.VirtualClusters[0].ClusterPolicies)
				policies[0] = editedJSON(policies[0], func(fields map[string]any) { fields[field] = value })
				drifted.VirtualClusters[0].ClusterPolicies = policies
				same, err := sameEntities(drifted, key, scopes...)
				require.NoError(t, err)
				require.False(t, same, "a changed %s must still be detected as drift", field)
			}

			reordered := held
			reordered.VirtualClusters = slices.Clone(held.VirtualClusters)
			reordered.VirtualClusters[0].ClusterPolicies = slices.Clone(held.VirtualClusters[0].ClusterPolicies)
			slices.Reverse(reordered.VirtualClusters[0].ClusterPolicies)
			same, err = sameEntities(reordered, key, scopes...)
			require.NoError(t, err)
			require.Equal(t, !managed, same, "default normalization must not change which scopes have order-sensitive comparison")
		})
	}
	unchanged, err := json.Marshal(pushed)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(unchanged), "default normalization must not mutate the desired document")
}

func TestComparisonPreservesOnlyExplicitListenerPolicyOrder(t *testing.T) {
	var entities gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal([]byte(`{
		"listeners":[
			{"id":"00000000-0000-4000-8000-000000000003","policies":[
				{"id":"00000000-0000-4000-8000-000000000051","type":"tls_server"},
				{"id":"00000000-0000-4000-8000-000000000052","type":"tls_server"}]},
			{"id":"00000000-0000-4000-8000-000000000004","policies":[
				{"id":"00000000-0000-4000-8000-000000000061","type":"tls_server"},
				{"id":"00000000-0000-4000-8000-000000000062","type":"tls_server"}]}]
	}`), &entities))
	listenerID := entities.Listeners[0].Id
	encoded, err := json.Marshal(entities)
	require.NoError(t, err)
	var held gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal(encoded, &held))
	slices.Reverse(held.Listeners[1].Policies)
	key, err := comparisonKey(entities, listenerID)
	require.NoError(t, err)
	same, err := sameEntities(held, key, listenerID)
	require.NoError(t, err)
	require.True(t, same, "the order of the policies of a listener without a chain is insignificant")

	slices.Reverse(held.Listeners[0].Policies)
	same, err = sameEntities(held, key, listenerID)
	require.NoError(t, err)
	require.False(t, same, "remote reordering of the policies a chain orders is drift")
}
