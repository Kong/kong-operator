package v1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestEventGatewayListenerPolicyChainPolicyOrder(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		policies []EventGatewayListenerPolicyRef
		wire     string
	}{
		{name: "ordered", policies: []EventGatewayListenerPolicyRef{{Name: "second"}, {Name: "first"}}, wire: `{"policies":[{"name":"second"},{"name":"first"}]}`},
		{name: "explicit empty", policies: []EventGatewayListenerPolicyRef{}, wire: `{"policies":[]}`},
		{name: "missing is not empty", wire: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			spec := EventGatewayListenerPolicyChainAPISpec{Policies: tt.policies}
			data, err := json.Marshal(spec)
			require.NoError(t, err)
			require.JSONEq(t, tt.wire, string(data))
			var decoded EventGatewayListenerPolicyChainAPISpec
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tt.policies, decoded.Policies)

			chain := &EventGatewayListenerPolicyChain{
				Spec: EventGatewayListenerPolicyChainSpec{APISpec: spec},
			}
			data, err = json.Marshal(chain)
			require.NoError(t, err)
			var decodedChain EventGatewayListenerPolicyChain
			require.NoError(t, json.Unmarshal(data, &decodedChain))
			require.Equal(t, tt.policies, decodedChain.Spec.APISpec.Policies)
			cloned := chain.DeepCopy()
			require.Equal(t, tt.policies, cloned.Spec.APISpec.Policies)
			if len(tt.policies) > 0 {
				cloned.Spec.APISpec.Policies[0].Name = "changed"
				require.Equal(t, "second", chain.Spec.APISpec.Policies[0].Name)
			}
		})
	}
	for _, wire := range []string{`{}`, `{"policies":null}`} {
		var spec EventGatewayListenerPolicyChainAPISpec
		require.NoError(t, json.Unmarshal([]byte(wire), &spec))
		require.Nil(t, spec.Policies, "missing and null must not become an explicit empty declaration")
	}
}

func TestEventGatewayListenerPolicyChainReferences(t *testing.T) {
	t.Parallel()

	parent := commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "listener"},
	}
	readError := errors.New("policy read unavailable")
	for _, tt := range []struct {
		name    string
		change  func(*EventGatewayListenerPolicyChain, *EventGatewayListenerPolicy)
		err     string
		readErr error
	}{
		{name: "ordered default references"},
		{name: "explicit kind and same namespace", change: func(c *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			c.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("default")
			c.Spec.APISpec.Policies[1].Kind = "EventGatewayListenerPolicy"
			c.Spec.APISpec.Policies[1].Namespace = "default"
			p.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("default")
		}},
		{name: "empty namespace defaults locally", change: func(c *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			c.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("")
			p.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("")
		}},
		{name: "explicit empty", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = []EventGatewayListenerPolicyRef{}
		}},
		{name: "missing list", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = nil
		}, err: "supplied explicitly"},
		{name: "missing parent", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.EventGatewayListenerRef.NamespacedRef = nil
		}, err: "same namespace"},
		{name: "parent by ID", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.EventGatewayListenerRef.Type = commonv1alpha1.ObjectRefTypeKonnectID
		}, err: "same namespace"},
		{name: "cross namespace parent", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("other")
		}, err: "same namespace"},
		{name: "empty policy name", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Name = ""
		}, err: "must name an EventGatewayListenerPolicy"},
		{name: "wrong kind", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterPolicy"
		}, err: "must name an EventGatewayListenerPolicy"},
		{name: "cross namespace policy", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, err: "cross-namespace"},
		{name: "duplicate default and explicit namespace", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = append(c.Spec.APISpec.Policies, EventGatewayListenerPolicyRef{Name: "first", Namespace: "default"})
		}, err: "duplicates"},
		{name: "absent policy", change: func(c *EventGatewayListenerPolicyChain, _ *EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Name = "absent"
		}, err: "not found"},
		{name: "different listener name", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.Spec.EventGatewayListenerRef.NamespacedRef.Name = "other"
		}, err: "different EventGatewayListener"},
		{name: "different listener namespace", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("other")
		}, err: "different EventGatewayListener"},
		{name: "different listener ID", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.SetEventGatewayListenerID("other")
		}, err: "different EventGatewayListener"},
		{name: "different gateway ID", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.SetGatewayID("other")
		}, err: "not referrer Gateway"},
		{name: "deleting policy", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.DeletionTimestamp = new(metav1.Now())
			p.Finalizers = []string{"test"}
		}, err: "being deleted"},
		{name: "unready policy", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.Status.Conditions[0].Status = metav1.ConditionFalse
		}, err: "not programmed"},
		{name: "stale generation", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.Generation++
		}, err: "not programmed"},
		{name: "missing ID", change: func(_ *EventGatewayListenerPolicyChain, p *EventGatewayListenerPolicy) {
			p.SetKonnectID("")
		}, err: "not programmed"},
		{name: "read error", readErr: readError, err: readError.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := &EventGatewayListenerPolicyChain{
				Namespace: "default",
				Spec: EventGatewayListenerPolicyChainSpec{
					EventGatewayListenerRef: *parent.DeepCopy(),
					APISpec: EventGatewayListenerPolicyChainAPISpec{
						Policies: []EventGatewayListenerPolicyRef{{Name: "second"}, {Name: "first"}},
					},
				},
			}
			chain.SetGatewayID("gateway-id")
			chain.SetEventGatewayListenerID("listener-id")
			first := &EventGatewayListenerPolicy{
				Name: "first", Namespace: chain.Namespace, Generation: 1,
				Spec: EventGatewayListenerPolicySpec{EventGatewayListenerRef: *parent.DeepCopy()},
			}
			first.SetKonnectID("first-id")
			first.SetGatewayID("gateway-id")
			first.SetEventGatewayListenerID("listener-id")
			first.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
			second := first.DeepCopy()
			second.Name = "second"
			second.SetKonnectID("second-id")
			if tt.change != nil {
				tt.change(chain, first)
			}
			s := runtime.NewScheme()
			require.NoError(t, AddToScheme(s))
			for _, entry := range []struct {
				name string
				call func(context.Context, client.Client) ([]string, error)
				ids  bool
			}{
				{name: "resolve validated", ids: true, call: chain.ResolveValidatedPolicyIDs},
				{name: "resolve generated", call: func(ctx context.Context, cl client.Client) ([]string, error) {
					return nil, chain.ResolveKonnectReferences(ctx, cl)
				}},
				{name: "create converter", ids: true, call: func(ctx context.Context, cl client.Client) ([]string, error) {
					req, err := chain.ToCreateUpdateEventGatewayListenerPolicyChainRequest(ctx, cl)
					if err != nil {
						require.Nil(t, req)
						return nil, err
					}
					require.NotNil(t, req.UpdateEventGatewayPolicyChainRequest)
					return req.UpdateEventGatewayPolicyChainRequest.Policies, nil
				}},
				{name: "update converter", ids: true, call: func(ctx context.Context, cl client.Client) ([]string, error) {
					req, err := chain.ToUpdateUpdateEventGatewayListenerPolicyChainRequest(ctx, cl)
					if err != nil {
						require.Nil(t, req)
						return nil, err
					}
					require.NotNil(t, req.UpdateEventGatewayPolicyChainRequest)
					return req.UpdateEventGatewayPolicyChainRequest.Policies, nil
				}},
			} {
				t.Run(entry.name, func(t *testing.T) {
					reads := make(map[string]int)
					cl := fake.NewClientBuilder().WithScheme(s).WithObjects(first, second).
						WithInterceptorFuncs(interceptor.Funcs{
							Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
								reads[key.Name]++
								if tt.readErr != nil {
									return tt.readErr
								}
								return cl.Get(ctx, key, obj, opts...)
							},
						}).Build()
					ids, err := entry.call(t.Context(), cl)
					if tt.err != "" {
						require.ErrorContains(t, err, tt.err)
						require.Nil(t, ids, "invalid references must not produce a shortened order")
						if tt.readErr != nil {
							require.ErrorIs(t, err, tt.readErr)
						}
						return
					}
					require.NoError(t, err)
					expectedReads := map[string]int{}
					expected := []string{}
					if len(chain.Spec.APISpec.Policies) > 0 {
						expectedReads = map[string]int{"first": 1, "second": 1}
						expected = []string{"second-id", "first-id"}
					}
					require.Equal(t, expectedReads, reads, "each policy is resolved once")
					if entry.ids {
						require.Equal(t, expected, ids)
						data, err := json.Marshal(ids)
						require.NoError(t, err)
						require.NotEqual(t, "null", string(data))
					}
				})
			}
			require.False(t, chain.PersistsKonnectID())
			require.Empty(t, chain.GetKonnectID())
		})
	}
}
