package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
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

func TestEventGatewayVirtualClusterPolicyChainReferences(t *testing.T) {
	t.Parallel()

	parent := commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: "vc",
		},
	}
	for _, tt := range []struct {
		name   string
		change func(*EventGatewayVirtualClusterPolicyChain, *EventGatewayVirtualClusterPolicy)
		err    string
	}{
		{name: "ordered references"},
		{name: "missing list", change: func(c *EventGatewayVirtualClusterPolicyChain, _ *EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies = nil
		}, err: "supplied explicitly"},
		{name: "duplicate", change: func(c *EventGatewayVirtualClusterPolicyChain, _ *EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies = append(c.Spec.APISpec.Policies, EventGatewayVirtualClusterPolicyRef{Name: "first"})
		}, err: "duplicates"},
		{name: "wrong kind", change: func(c *EventGatewayVirtualClusterPolicyChain, _ *EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayListenerPolicy"
		}, err: "must name an EventGatewayVirtualClusterPolicy"},
		{name: "wrong namespace", change: func(c *EventGatewayVirtualClusterPolicyChain, _ *EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, err: "cross-namespace"},
		{name: "missing policy", change: func(c *EventGatewayVirtualClusterPolicyChain, _ *EventGatewayVirtualClusterPolicy) {
			c.Spec.APISpec.Policies[0].Name = "missing"
		}, err: "not found"},
		{name: "wrong parent", change: func(_ *EventGatewayVirtualClusterPolicyChain, p *EventGatewayVirtualClusterPolicy) {
			p.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
		}, err: "different EventGatewayVirtualCluster"},
		{name: "unready", change: func(_ *EventGatewayVirtualClusterPolicyChain, p *EventGatewayVirtualClusterPolicy) {
			p.Status.Conditions[0].Status = metav1.ConditionFalse
		}, err: "not programmed"},
		{name: "stale readiness", change: func(_ *EventGatewayVirtualClusterPolicyChain, p *EventGatewayVirtualClusterPolicy) {
			p.Generation++
		}, err: "not programmed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &EventGatewayVirtualClusterPolicyChain{
				Namespace: "default",
				Spec: EventGatewayVirtualClusterPolicyChainSpec{
					EventGatewayVirtualClusterRef: *parent.DeepCopy(),
					APISpec: EventGatewayVirtualClusterPolicyChainAPISpec{
						Policies: []EventGatewayVirtualClusterPolicyRef{{Name: "second"}, {Name: "first"}},
					},
				},
			}
			first := &EventGatewayVirtualClusterPolicy{
				Name: "first", Namespace: "default", Generation: 1,
				Spec: EventGatewayVirtualClusterPolicySpec{EventGatewayVirtualClusterRef: *parent.DeepCopy()},
			}
			first.SetKonnectID("first-id")
			first.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
			second := first.DeepCopy()
			second.Name = "second"
			second.SetKonnectID("second-id")
			if tt.change != nil {
				tt.change(c, first)
			}
			s := runtime.NewScheme()
			require.NoError(t, AddToScheme(s))
			cl := fake.NewClientBuilder().WithScheme(s).WithObjects(first, second).Build()
			err := c.ValidatePolicyReferences(t.Context(), cl)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			req, err := c.ToUpdateUpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest(t.Context(), cl)
			require.NoError(t, err)
			require.Equal(t, []string{"second-id", "first-id"}, req.UpdateEventGatewayPolicyChainRequest.Policies)
			require.False(t, c.PersistsKonnectID())
		})
	}
}
