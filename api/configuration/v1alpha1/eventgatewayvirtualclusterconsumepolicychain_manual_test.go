package v1alpha1

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func TestEventGatewayVirtualClusterConsumePolicyChainPolicyOrder(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		policies []EventGatewayVirtualClusterConsumePolicyRef
		wire     string
	}{
		{
			name: "root groups and nested sibling order",
			policies: []EventGatewayVirtualClusterConsumePolicyRef{
				{Name: "parent"}, {Name: "second-child"}, {Name: "first-child"},
				{Name: "other-parent"}, {Name: "other-child"}, {Name: "root"},
			},
			wire: `{"policies":[{"name":"parent"},{"name":"second-child"},{"name":"first-child"},{"name":"other-parent"},{"name":"other-child"},{"name":"root"}]}`,
		},
		{name: "explicit empty", policies: []EventGatewayVirtualClusterConsumePolicyRef{}, wire: `{"policies":[]}`},
		{name: "missing is not empty", wire: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := &EventGatewayVirtualClusterConsumePolicyChain{
				Spec: EventGatewayVirtualClusterConsumePolicyChainSpec{
					APISpec: EventGatewayVirtualClusterConsumePolicyChainAPISpec{Policies: tt.policies},
				},
			}
			data, err := json.Marshal(chain.Spec.APISpec)
			require.NoError(t, err)
			require.JSONEq(t, tt.wire, string(data))
			data, err = json.Marshal(chain)
			require.NoError(t, err)
			var decoded EventGatewayVirtualClusterConsumePolicyChain
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Equal(t, tt.policies, decoded.Spec.APISpec.Policies)
			cloned := chain.DeepCopy()
			require.Equal(t, tt.policies, cloned.Spec.APISpec.Policies)
			if len(tt.policies) > 0 {
				cloned.Spec.APISpec.Policies[0].Name = "changed"
				require.Equal(t, "parent", chain.Spec.APISpec.Policies[0].Name)
			}
		})
	}
	for _, wire := range []string{`{}`, `{"policies":null}`} {
		var spec EventGatewayVirtualClusterConsumePolicyChainAPISpec
		require.NoError(t, json.Unmarshal([]byte(wire), &spec))
		require.Nil(t, spec.Policies, "missing and null must not become an explicit empty declaration")
	}
}

func TestEventGatewayVirtualClusterConsumePolicyChainMetadata(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, AddToScheme(s))
	for _, kind := range []string{
		"EventGatewayVirtualClusterConsumePolicyChain",
		"EventGatewayVirtualClusterConsumePolicyChainList",
	} {
		_, err := s.New(GroupVersion.WithKind(kind))
		require.NoError(t, err)
	}
	chain := &EventGatewayVirtualClusterConsumePolicyChain{}
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
	require.False(t, chain.PersistsKonnectID())
}

func TestEventGatewayVirtualClusterConsumePolicyChainReferences(t *testing.T) {
	t.Parallel()
	readErr := errors.New("policy read unavailable")
	for _, tt := range []struct {
		name   string
		order  []string
		change func(*EventGatewayVirtualClusterConsumePolicyChain, map[string]*EventGatewayVirtualClusterConsumePolicy)
		err    string
		fail   bool
	}{
		{name: "all current variants"},
		{name: "move whole groups and reorder siblings", order: []string{"root", "other", "headers", "parent", "skip", "transcode", "mask", "decrypt"}},
		{name: "skipRecord can be a root", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["skip"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{}
		}, order: []string{"parent", "mask", "transcode", "decrypt", "skip", "other", "headers", "root"}},
		{name: "explicit namespaces", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("default")
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterConsumePolicy"
			c.Spec.APISpec.Policies[0].Namespace = "default"
			p["mask"].Spec.APISpec.ParentPolicyRef.Kind = "EventGatewayVirtualClusterConsumePolicy"
			p["mask"].Spec.APISpec.ParentPolicyRef.Namespace = "default"
		}},
		{name: "empty namespace defaults locally", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("")
			p["mask"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("")
		}},
		{name: "empty scope", order: []string{}},
		{name: "omitted list", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.APISpec.Policies = nil
		}, err: "supplied explicitly"},
		{name: "cross namespace VC", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("foreign")
		}, err: "same namespace"},
		{name: "VC by ID", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.Type = commonv1alpha1.ObjectRefTypeKonnectID
		}, err: "same namespace"},
		{name: "empty name", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.APISpec.Policies[0].Name = ""
		}, err: "must name"},
		{name: "wrong kind", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterProducePolicy"
		}, err: "must name"},
		{name: "cross namespace policy", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "foreign"
		}, err: "cross-namespace"},
		{name: "duplicate name", order: []string{"parent", "mask", "mask"}, err: "duplicates policy"},
		{name: "duplicate ID", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].SetKonnectID("parent-id")
		}, err: "duplicates Konnect policy ID"},
		{name: "not found", order: []string{"parent", "missing"}, err: "not found"},
		{name: "foreign VC", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "foreign"
		}, err: "different EventGatewayVirtualCluster"},
		{name: "foreign VC namespace", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("foreign")
		}, err: "different EventGatewayVirtualCluster"},
		{name: "foreign VC ID", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].SetVirtualClusterID("foreign")
		}, err: "different EventGatewayVirtualCluster"},
		{name: "foreign gateway", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].SetGatewayID("foreign")
		}, err: "not referrer Gateway"},
		{name: "foreign organization", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.GetKonnectStatus().SetOrgID("chain-org")
			for _, policy := range p {
				policy.GetKonnectStatus().SetOrgID("chain-org")
			}
			p["mask"].GetKonnectStatus().SetOrgID("foreign-org")
		}, err: "organization"},
		{name: "consistent organization without chain status", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			for _, policy := range p {
				policy.GetKonnectStatus().SetOrgID("chain-org")
			}
		}},
		{name: "inconsistent organizations without chain status", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["parent"].GetKonnectStatus().SetOrgID("chain-org")
			p["mask"].GetKonnectStatus().SetOrgID("foreign-org")
		}, err: "organization"},
		{name: "unknown policy organization", change: func(c *EventGatewayVirtualClusterConsumePolicyChain, _ map[string]*EventGatewayVirtualClusterConsumePolicy) {
			c.GetKonnectStatus().SetOrgID("chain-org")
		}},
		{name: "stale child", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Generation++
		}, err: "not programmed"},
		{name: "stale parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["parent"].Generation++
		}, err: "not programmed"},
		{name: "unready", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Status.Conditions[0].Status = metav1.ConditionFalse
		}, err: "not programmed"},
		{name: "missing ID", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].SetKonnectID("")
		}, err: "not programmed"},
		{name: "terminating", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].DeletionTimestamp = new(metav1.Now())
		}, err: "being deleted"},
		{name: "missing parent in declaration", order: []string{"mask"}, err: "must be included"},
		{name: "child before parent", order: []string{"mask", "parent"}, err: "contiguous group"},
		{name: "split group", order: []string{"parent", "mask", "root", "skip"}, err: "contiguous group"},
		{name: "interleaved groups", order: []string{"parent", "mask", "other", "transcode", "headers"}, err: "contiguous group"},
		{name: "self parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.ParentPolicyRef.Name = "mask"
		}, err: "cannot reference itself"},
		{name: "wrong parent kind", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.ParentPolicyRef.Kind = "EventGatewayVirtualClusterProducePolicy"
		}, err: "same namespace"},
		{name: "wrong parent namespace", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.ParentPolicyRef.Namespace = "foreign"
		}, err: "same namespace"},
		{name: "malformed parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{Kind: "EventGatewayVirtualClusterConsumePolicy"}
		}, err: "valid parentPolicyRef"},
		{name: "deeper nesting", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["transcode"].Spec.APISpec.ParentPolicyRef.Name = "mask"
		}, err: "root schemaValidation"},
		{name: "schemaValidation cannot be nested", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["parent"].Spec.APISpec.ParentPolicyRef.Name = "other"
		}, err: "only supported"},
		{name: "decrypt is root only", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["root"].Spec.APISpec.ParentPolicyRef.Name = "parent"
		}, err: "only supported"},
		{name: "missing config", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig = nil
		}, err: "supported apiSpec.type"},
		{name: "mismatched config", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.Type = EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate
		}, err: "supported apiSpec.type"},
		{name: "unknown variant", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.Type = "unknown"
		}, err: "supported apiSpec.type"},
		{name: "multiple union members", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.SkipRecordPolicyCreate = &EventGatewaySkipRecordPolicyCreate{}
		}, err: "exactly its configuration"},
		{name: "decryptFields requires parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["decrypt"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "maskFields requires parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["mask"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "transcode requires parent", change: func(_ *EventGatewayVirtualClusterConsumePolicyChain, p map[string]*EventGatewayVirtualClusterConsumePolicy) {
			p["transcode"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "read error", fail: true, err: readErr.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := &EventGatewayVirtualClusterConsumePolicyChain{Namespace: "default",
				Spec: EventGatewayVirtualClusterConsumePolicyChainSpec{
					EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc"}},
					APISpec:                       EventGatewayVirtualClusterConsumePolicyChainAPISpec{Policies: []EventGatewayVirtualClusterConsumePolicyRef{}},
				},
			}
			chain.SetGatewayID("gateway-id")
			chain.SetVirtualClusterID("vc-id")
			policies := consumeChainPolicies(t, chain.GetParentRef())
			order := tt.order
			if order == nil {
				order = []string{"parent", "mask", "transcode", "decrypt", "skip", "other", "headers", "root"}
			}
			for _, name := range order {
				chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, EventGatewayVirtualClusterConsumePolicyRef{Name: name})
			}
			if tt.change != nil {
				tt.change(chain, policies)
			}
			before := chain.DeepCopy()
			for _, converter := range []string{"resolver", "create", "update"} {
				t.Run(converter, func(t *testing.T) {
					reads := map[string]int{}
					cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
						Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
							reads[key.Name]++
							if tt.fail {
								return readErr
							}
							p, ok := policies[key.Name]
							if !ok {
								return apierrors.NewNotFound(GroupVersion.WithResource("eventgatewayvirtualclusterconsumepolicies").GroupResource(), key.Name)
							}
							p.DeepCopyInto(obj.(*EventGatewayVirtualClusterConsumePolicy))
							return nil
						},
					}).Build()
					var ids []string
					var err error
					switch converter {
					case "resolver":
						ids, err = chain.ResolveValidatedPolicyIDs(t.Context(), cl)
					case "create":
						req, e := chain.ToCreateUpdateEventGatewayVirtualClusterConsumePolicyChainRequest(t.Context(), cl)
						err = e
						if req != nil {
							ids = req.UpdateEventGatewayPolicyChainRequest.Policies
						}
					case "update":
						req, e := chain.ToUpdateUpdateEventGatewayVirtualClusterConsumePolicyChainRequest(t.Context(), cl)
						err = e
						if req != nil {
							ids = req.UpdateEventGatewayPolicyChainRequest.Policies
						}
					}
					for name, count := range reads {
						require.Equal(t, 1, count, "parent snapshots must not be re-read: %s", name)
					}
					require.Equal(t, before, chain, "resolution must not mutate references or IDs")
					if tt.err != "" {
						require.ErrorContains(t, err, tt.err)
						require.Nil(t, ids, "invalid references must never yield a reduced chain")
						if tt.fail {
							require.ErrorIs(t, err, readErr)
						}
						return
					}
					require.NoError(t, err)
					want := make([]string, 0, len(chain.Spec.APISpec.Policies))
					for _, ref := range chain.Spec.APISpec.Policies {
						want = append(want, policies[ref.Name].GetKonnectID())
					}
					require.Equal(t, want, ids)
					require.Len(t, reads, len(want))
				})
			}
		})
	}
}

func consumeChainPolicies(t *testing.T, parent commonv1alpha1.ObjectRef) map[string]*EventGatewayVirtualClusterConsumePolicy {
	t.Helper()
	policies := make(map[string]*EventGatewayVirtualClusterConsumePolicy)
	for name, spec := range map[string]string{
		"parent": `type: schemaValidation
schemaValidation: {name: parent, config: {type: json, json: {valueValidationAction: skip}}}`,
		"other": `type: schemaValidation
schemaValidation: {name: other, config: {type: json, json: {valueValidationAction: skip}}}`,
		"mask": `type: maskFields
parentPolicyRef: {name: parent}
maskFields: {name: mask, config: {failureMode: skip, maskFields: [{paths: {type: array, array: [{match: email}]}, strategy: {type: replace, replace: {phrase: redacted}}}]}}`,
		"transcode": `type: transcode
parentPolicyRef: {name: parent}
transcode:
  name: transcode
  config:
    outputFormat: json
    json:
      failureMode: skip
      schemaSource:
        type: reference
        reference:
          subject: orders-value
          version: latest
          schemaRefDestination: {type: none, none: {}}
          schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}`,
		"decrypt": `type: decryptFields
parentPolicyRef: {name: parent}
decryptFields: {name: decrypt, config: {failureMode: skip, decryptFields: {paths: {type: array, array: [{match: email}]}}, keySources: [{type: static, static: {}}]}}`,
		"skip": `type: skipRecord
parentPolicyRef: {name: parent}
skipRecord: {name: skip}`,
		"headers": `type: modifyHeaders
parentPolicyRef: {name: other}
modifyHeaders: {name: headers, config: {actions: [{op: set, set: {key: x-chain, value: example}}]}}`,
		"root": `type: decrypt
decrypt: {name: root, config: {failureMode: passthrough, partOfRecord: [value], keySources: [{type: static, static: {}}]}}`,
	} {
		p := &EventGatewayVirtualClusterConsumePolicy{Name: name, Namespace: "default", Generation: 1,
			Spec: EventGatewayVirtualClusterConsumePolicySpec{EventGatewayVirtualClusterRef: *parent.DeepCopy()},
		}
		require.NoError(t, yaml.UnmarshalStrict([]byte(spec), &p.Spec.APISpec))
		p.SetGatewayID("gateway-id")
		p.SetVirtualClusterID("vc-id")
		p.SetKonnectID(name + "-id")
		p.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}}
		policies[name] = p
	}
	return policies
}
