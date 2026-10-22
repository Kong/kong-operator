package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventGatewayVirtualClusterPolicyChainPolicyOrder(t *testing.T) {
	t.Parallel()

	for _, policies := range [][]EventGatewayVirtualClusterPolicyRef{
		{{Name: "second"}, {Name: "first"}},
		{},
	} {
		spec := EventGatewayVirtualClusterPolicyChainAPISpec{Policies: policies}
		data, err := json.Marshal(spec)
		require.NoError(t, err)
		require.Contains(t, string(data), `"policies":[`)

		var decoded EventGatewayVirtualClusterPolicyChainAPISpec
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Equal(t, policies, decoded.Policies)

		chain := &EventGatewayVirtualClusterPolicyChain{
			Spec: EventGatewayVirtualClusterPolicyChainSpec{APISpec: spec},
		}
		chainData, err := json.Marshal(chain)
		require.NoError(t, err)
		require.Contains(t, string(chainData), `"apiSpec":{"policies":[`)
		var decodedChain EventGatewayVirtualClusterPolicyChain
		require.NoError(t, json.Unmarshal(chainData, &decodedChain))
		require.Equal(t, policies, decodedChain.Spec.APISpec.Policies)

		cloned := chain.DeepCopy()
		require.Equal(t, policies, cloned.Spec.APISpec.Policies)
		if len(policies) > 0 {
			cloned.Spec.APISpec.Policies[0].Name = "changed"
			require.Equal(t, "second", chain.Spec.APISpec.Policies[0].Name)
		}
	}
}
