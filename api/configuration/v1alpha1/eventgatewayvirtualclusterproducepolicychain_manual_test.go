package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestEventGatewayVirtualClusterProducePolicyChainPolicyOrder(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		policies []EventGatewayVirtualClusterProducePolicyRef
		wire     string
	}{
		{
			name: "root and nested sibling order",
			policies: []EventGatewayVirtualClusterProducePolicyRef{
				{Name: "parent"}, {Name: "second-child"}, {Name: "first-child"}, {Name: "root"},
			},
			wire: `{"policies":[{"name":"parent"},{"name":"second-child"},{"name":"first-child"},{"name":"root"}]}`,
		},
		{name: "explicit empty", policies: []EventGatewayVirtualClusterProducePolicyRef{}, wire: `{"policies":[]}`},
		{name: "missing is not empty", wire: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := EventGatewayVirtualClusterProducePolicyChainAPISpec{Policies: tt.policies}
			data, err := json.Marshal(spec)
			require.NoError(t, err)
			require.JSONEq(t, tt.wire, string(data))
			var decoded EventGatewayVirtualClusterProducePolicyChainAPISpec
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tt.policies, decoded.Policies)

			chain := &EventGatewayVirtualClusterProducePolicyChain{
				Spec: EventGatewayVirtualClusterProducePolicyChainSpec{APISpec: spec},
			}
			data, err = json.Marshal(chain)
			require.NoError(t, err)
			var decodedChain EventGatewayVirtualClusterProducePolicyChain
			require.NoError(t, json.Unmarshal(data, &decodedChain))
			require.Equal(t, tt.policies, decodedChain.Spec.APISpec.Policies)
			cloned := chain.DeepCopy()
			require.Equal(t, tt.policies, cloned.Spec.APISpec.Policies)
			if len(tt.policies) > 0 {
				cloned.Spec.APISpec.Policies[0].Name = "changed"
				require.Equal(t, "parent", chain.Spec.APISpec.Policies[0].Name)
			}
		})
	}
	for _, wire := range []string{`{}`, `{"policies":null}`} {
		var spec EventGatewayVirtualClusterProducePolicyChainAPISpec
		require.NoError(t, json.Unmarshal([]byte(wire), &spec))
		require.Nil(t, spec.Policies, "missing and null must not become an explicit empty declaration")
	}
}

func TestEventGatewayVirtualClusterProducePolicyChainMetadata(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, AddToScheme(s))
	for _, kind := range []string{
		"EventGatewayVirtualClusterProducePolicyChain",
		"EventGatewayVirtualClusterProducePolicyChainList",
	} {
		_, err := s.New(GroupVersion.WithKind(kind))
		require.NoError(t, err)
	}
	chain := &EventGatewayVirtualClusterProducePolicyChain{}
	parent := commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "virtual-cluster"},
	}
	chain.SetParentRef(parent)
	require.Equal(t, parent, chain.Spec.EventGatewayVirtualClusterRef)
	require.Equal(t, parent, chain.GetParentRef())
	require.Equal(t, parent, chain.GetEventGatewayVirtualClusterRef())
	require.Equal(t, GroupVersion.WithKind("EventGatewayVirtualCluster"), chain.GetParentGVK())
	chain.SetParentID("virtual-cluster-id")
	chain.SetGatewayID("gateway-id")
	require.Equal(t, "virtual-cluster-id", chain.GetVirtualClusterID())
	require.Equal(t, "gateway-id", chain.GetGatewayID())
	require.Empty(t, chain.GetKonnectID(), "a chain has no independent ID")
}
