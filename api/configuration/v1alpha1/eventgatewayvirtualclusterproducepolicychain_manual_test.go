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
	require.False(t, chain.PersistsKonnectID())
	require.Equal(t, parent, chain.GetVirtualClusterRef())
	require.Equal(t, map[string]string{"KonnectEventGateway": "gateway-id"}, chain.GetAncestorIDs())
}

func TestEventGatewayVirtualClusterProducePolicyChainReferences(t *testing.T) {
	t.Parallel()
	readErr := errors.New("policy read unavailable")
	for _, tt := range []struct {
		name   string
		change func(*EventGatewayVirtualClusterProducePolicyChain, map[string]*EventGatewayVirtualClusterProducePolicy)
		order  []string
		err    string
		fail   bool
	}{
		{name: "nested siblings", order: []string{"parent", "second", "first", "root"}},
		{name: "root groups move", order: []string{"root", "parent", "first", "second"}},
		{name: "two parent groups move", order: []string{"root", "second", "parent", "first"}, change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["root"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = p["parent"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig.DeepCopy()
			p["second"].Spec.APISpec.ParentPolicyRef.Name = "root"
		}},
		{name: "nested encryptFields", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type:                                  EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate,
				ParsedRecordEncryptFieldsPolicyCreate: &EventGatewayParsedRecordEncryptFieldsPolicyCreate{},
			}
		}},
		{name: "nested maskFields", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate,
				ParsedRecordMaskFieldsProducePolicyCreate: &EventGatewayParsedRecordMaskFieldsProducePolicyCreate{},
			}
		}},
		{name: "nested transcode", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type:                                     EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate,
				ParsedRecordTranscodeProducePolicyCreate: &EventGatewayParsedRecordTranscodeProducePolicyCreate{},
			}
		}},
		{name: "maskFields needs schemaValidation parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate,
				ParsedRecordMaskFieldsProducePolicyCreate: &EventGatewayParsedRecordMaskFieldsProducePolicyCreate{},
			}
			p["first"].Spec.APISpec.ParentPolicyRef.Name = "root"
		}, err: "root schemaValidation"},
		{name: "transcode cannot interleave root", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type:                                     EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate,
				ParsedRecordTranscodeProducePolicyCreate: &EventGatewayParsedRecordTranscodeProducePolicyCreate{},
			}
		}, order: []string{"parent", "second", "root", "first"}, err: "contiguous group"},
		{name: "explicit kind and namespace", change: func(c *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("default")
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterProducePolicy"
			c.Spec.APISpec.Policies[0].Namespace = "default"
			p["first"].Spec.APISpec.ParentPolicyRef.Kind = "EventGatewayVirtualClusterProducePolicy"
			p["first"].Spec.APISpec.ParentPolicyRef.Namespace = "default"
		}},
		{name: "empty namespace", change: func(c *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("")
			p["first"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("")
		}},
		{name: "explicit empty", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = []EventGatewayVirtualClusterProducePolicyRef{}
		}},
		{name: "missing list", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies = nil
		}, err: "supplied explicitly"},
		{name: "cross namespace parent", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("other")
		}, err: "same namespace"},
		{name: "parent by ID", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.EventGatewayVirtualClusterRef.Type = commonv1alpha1.ObjectRefTypeKonnectID
		}, err: "same namespace"},
		{name: "missing name", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0].Name = ""
		}, err: "must name"},
		{name: "wrong kind", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, err: "must name"},
		{name: "cross namespace policy", change: func(c *EventGatewayVirtualClusterProducePolicyChain, _ map[string]*EventGatewayVirtualClusterProducePolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, err: "cross-namespace"},
		{name: "duplicate name", order: []string{"parent", "first", "second", "first"}, err: "duplicates policy"},
		{name: "duplicate ID", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].SetKonnectID("second-id")
		}, err: "duplicates Konnect policy ID"},
		{name: "missing reference", order: []string{"parent", "missing"}, err: "not found"},
		{name: "wrong virtual cluster", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
		}, err: "different EventGatewayVirtualCluster"},
		{name: "wrong virtual cluster ID", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].SetVirtualClusterID("other")
		}, err: "different EventGatewayVirtualCluster"},
		{name: "wrong gateway", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].SetGatewayID("other")
		}, err: "not referrer Gateway"},
		{name: "stale child", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Generation++
		}, err: "not programmed"},
		{name: "stale parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["parent"].Generation++
		}, err: "not programmed"},
		{name: "missing ID", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].SetKonnectID("")
		}, err: "not programmed"},
		{name: "new policy before ancestry resolution", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].SetKonnectID("")
			p["first"].SetGatewayID("")
			p["first"].SetVirtualClusterID("")
			p["first"].Status.Conditions = nil
		}, err: "not programmed"},
		{name: "deleting parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["parent"].DeletionTimestamp = new(metav1.Now())
			p["parent"].Finalizers = []string{"test"}
		}, err: "being deleted"},
		{name: "deleting child", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].DeletionTimestamp = new(metav1.Now())
			p["first"].Finalizers = []string{"test"}
		}, err: "being deleted"},
		{name: "unready child", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Status.Conditions[0].Status = metav1.ConditionFalse
		}, err: "not programmed"},
		{name: "policy declares foreign namespace", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("other")
		}, err: "different EventGatewayVirtualCluster"},
		{name: "children only", order: []string{"first", "second"}, err: "must be included"},
		{name: "child before parent", order: []string{"first", "parent", "second", "root"}, err: "contiguous group"},
		{name: "interleaved root", order: []string{"parent", "first", "root", "second"}, err: "contiguous group"},
		{name: "cross parent interleaving", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["second"].Spec.APISpec.ParentPolicyRef.Name = "root"
			p["root"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = p["parent"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig.DeepCopy()
		}, err: "contiguous group"},
		{name: "wrong parent kind", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Kind = "EventGatewayVirtualClusterConsumePolicy"
		}, err: "same namespace"},
		{name: "wrong parent namespace", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Namespace = "other"
		}, err: "same namespace"},
		{name: "self parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.ParentPolicyRef.Name = "first"
		}, err: "cannot reference itself"},
		{name: "cycle", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["parent"].Spec.APISpec.ParentPolicyRef.Name = "first"
		}, err: "only supported"},
		{name: "deeper nesting", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["second"].Spec.APISpec.ParentPolicyRef.Name = "first"
		}, err: "root schemaValidation"},
		{name: "missing configuration", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = nil
		}, err: "supported apiSpec.type"},
		{name: "wrong configuration payload", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.Type = EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy
		}, err: "supported apiSpec.type"},
		{name: "encryptFields requires parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type:                                  EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordEncryptFieldsPolicyCreate,
				ParsedRecordEncryptFieldsPolicyCreate: &EventGatewayParsedRecordEncryptFieldsPolicyCreate{},
			}
			p["first"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "maskFields requires parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate,
				ParsedRecordMaskFieldsProducePolicyCreate: &EventGatewayParsedRecordMaskFieldsProducePolicyCreate{},
			}
			p["first"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "transcode requires parent", change: func(_ *EventGatewayVirtualClusterProducePolicyChain, p map[string]*EventGatewayVirtualClusterProducePolicy) {
			p["first"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type:                                     EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate,
				ParsedRecordTranscodeProducePolicyCreate: &EventGatewayParsedRecordTranscodeProducePolicyCreate{},
			}
			p["first"].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{}
		}, err: "valid parentPolicyRef"},
		{name: "read error", fail: true, err: readErr.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc"}}
			chain := &EventGatewayVirtualClusterProducePolicyChain{Namespace: "default",
				Spec: EventGatewayVirtualClusterProducePolicyChainSpec{EventGatewayVirtualClusterRef: parent},
			}
			chain.SetGatewayID("gateway-id")
			chain.SetVirtualClusterID("vc-id")
			order := tt.order
			if order == nil {
				order = []string{"parent", "second", "first", "root"}
			}
			for _, name := range order {
				chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, EventGatewayVirtualClusterProducePolicyRef{Name: name})
			}
			policies := make(map[string]*EventGatewayVirtualClusterProducePolicy)
			for _, name := range []string{"parent", "first", "second", "root"} {
				p := &EventGatewayVirtualClusterProducePolicy{Name: name, Namespace: chain.Namespace, Generation: 1,
					Spec: EventGatewayVirtualClusterProducePolicySpec{EventGatewayVirtualClusterRef: *parent.DeepCopy(),
						APISpec: EventGatewayVirtualClusterProducePolicyAPISpec{EventGatewayVirtualClusterProducePolicyConfig: &EventGatewayVirtualClusterProducePolicyConfig{
							Type:                      EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
							ModifyHeadersPolicyCreate: &EventGatewayModifyHeadersPolicyCreate{},
						}},
					},
				}
				p.SetGatewayID("gateway-id")
				p.SetVirtualClusterID("vc-id")
				p.SetKonnectID(name + "-id")
				p.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation}}
				policies[name] = p
			}
			policies["parent"].Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &EventGatewayVirtualClusterProducePolicyConfig{
				Type: EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy, ProduceSchemaValidationPolicy: &EventGatewayProduceSchemaValidationPolicy{},
			}
			for _, name := range []string{"first", "second"} {
				policies[name].Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{Name: "parent"}
			}
			if tt.change != nil {
				tt.change(chain, policies)
			}
			s := runtime.NewScheme()
			require.NoError(t, AddToScheme(s))
			var objects []client.Object
			for _, policy := range policies {
				objects = append(objects, policy)
			}
			for _, converter := range []string{"resolver", "create", "update"} {
				t.Run(converter, func(t *testing.T) {
					reads := make(map[string]int)
					cl := fake.NewClientBuilder().WithScheme(s).WithObjects(objects...).
						WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							reads[key.Name]++
							if tt.fail {
								return readErr
							}
							return cl.Get(ctx, key, obj, opts...)
						}}).Build()
					var ids []string
					var err error
					switch converter {
					case "resolver":
						ids, err = chain.ResolveValidatedPolicyIDs(t.Context(), cl)
					case "create":
						req, e := chain.ToCreateUpdateEventGatewayVirtualClusterProducePolicyChainRequest(t.Context(), cl)
						err = e
						if req != nil {
							ids = req.UpdateEventGatewayPolicyChainRequest.Policies
						}
					case "update":
						req, e := chain.ToUpdateUpdateEventGatewayVirtualClusterProducePolicyChainRequest(t.Context(), cl)
						err = e
						if req != nil {
							ids = req.UpdateEventGatewayPolicyChainRequest.Policies
						}
					}
					for name, count := range reads {
						require.Equal(t, 1, count, "snapshot for %s must not be re-read as a parent", name)
					}
					resolverReads := len(reads)
					if !tt.fail {
						for name, original := range policies {
							var current EventGatewayVirtualClusterProducePolicy
							require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(original), &current))
							before, err := json.Marshal(original.Spec)
							require.NoError(t, err)
							after, err := json.Marshal(current.Spec)
							require.NoError(t, err)
							require.JSONEq(t, string(before), string(after), "ordering must not rewrite %s parentPolicyRef or configuration", name)
							require.Equal(t, original.GetKonnectID(), current.GetKonnectID())
						}
					}
					if tt.err != "" {
						require.ErrorContains(t, err, tt.err)
						require.Nil(t, ids, "invalid declarations cannot yield shortened chains")
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
					require.Equal(t, len(chain.Spec.APISpec.Policies), resolverReads)
				})
			}
		})
	}
}
