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
