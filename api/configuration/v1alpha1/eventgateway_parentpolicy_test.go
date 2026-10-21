package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

func policyVirtualClusterRef(name string) commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: name},
	}
}

func TestEventGatewayParentPolicyRequests(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	produce := &EventGatewayVirtualClusterProducePolicy{
		Name: "parent", Namespace: "default",
		Spec: EventGatewayVirtualClusterProducePolicySpec{
			EventGatewayVirtualClusterRef: policyVirtualClusterRef("vc"),
			APISpec: EventGatewayVirtualClusterProducePolicyAPISpec{
				EventGatewayVirtualClusterProducePolicyConfig: &EventGatewayVirtualClusterProducePolicyConfig{
					Type: EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy,
				},
			},
		},
	}
	produce.SetKonnectID("parent-id")
	consume := &EventGatewayVirtualClusterConsumePolicy{
		ObjectMeta: produce.ObjectMeta,
		Spec: EventGatewayVirtualClusterConsumePolicySpec{
			EventGatewayVirtualClusterRef: policyVirtualClusterRef("vc"),
			APISpec: EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &EventGatewayVirtualClusterConsumePolicyConfig{
					Type: EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy,
				},
			},
		},
	}
	consume.SetKonnectID("parent-id")
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(produce, consume).Build()

	for _, tc := range []struct {
		name    string
		produce bool
		spec    string
	}{
		{
			name: "encryptFields", produce: true,
			spec: `{"type":"encryptFields","encryptFields":{"name":"encrypt","config":{"failureMode":"reject","encryptFields":[{"paths":{"type":"array","array":[{"match":"secret"}]},"encryptionKey":{"type":"static","static":{"key":{"id":"key-id"}}}}]}}}`,
		},
		{
			name: "produce modifyHeaders", produce: true,
			spec: `{"type":"modifyHeaders","modifyHeaders":{"config":{"actions":[{"op":"set","set":{"key":"x-test","value":"test"}}]}}}`,
		},
		{
			name: "decryptFields",
			spec: `{"type":"decryptFields","decryptFields":{"name":"decrypt","config":{"failureMode":"error","decryptFields":{"paths":{"type":"array","array":[{"match":"secret"}]}},"keySources":[{"type":"static","static":{}}]}}}`,
		},
		{
			name: "consume modifyHeaders",
			spec: `{"type":"modifyHeaders","modifyHeaders":{"config":{"actions":[{"op":"set","set":{"key":"x-test","value":"test"}}]}}}`,
		},
		{
			name: "skipRecord",
			spec: `{"type":"skipRecord","skipRecord":{"condition":"true"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body any
			if tc.produce {
				obj := &EventGatewayVirtualClusterProducePolicy{
					Name: "child", Namespace: "default",
					Spec: EventGatewayVirtualClusterProducePolicySpec{EventGatewayVirtualClusterRef: policyVirtualClusterRef("vc")},
				}
				require.NoError(t, json.Unmarshal([]byte(tc.spec), &obj.Spec.APISpec))
				obj.Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{Name: "parent"}
				data, err := json.Marshal(obj)
				require.NoError(t, err)
				var decoded EventGatewayVirtualClusterProducePolicy
				require.NoError(t, json.Unmarshal(data, &decoded))
				require.Equal(t, obj.Spec, decoded.Spec)
				req, err := decoded.ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
				require.NoError(t, err)
				body = req.EventGatewayProducePolicyCreate
				_, err = decoded.ToUpdateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
				require.NoError(t, err)
			} else {
				obj := &EventGatewayVirtualClusterConsumePolicy{
					Name: "child", Namespace: "default",
					Spec: EventGatewayVirtualClusterConsumePolicySpec{EventGatewayVirtualClusterRef: policyVirtualClusterRef("vc")},
				}
				require.NoError(t, json.Unmarshal([]byte(tc.spec), &obj.Spec.APISpec))
				obj.Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{Name: "parent"}
				data, err := json.Marshal(obj)
				require.NoError(t, err)
				var decoded EventGatewayVirtualClusterConsumePolicy
				require.NoError(t, json.Unmarshal(data, &decoded))
				require.Equal(t, obj.Spec, decoded.Spec)
				req, err := decoded.ToCreateEventGatewayVirtualClusterConsumePolicyRequest(t.Context(), cl)
				require.NoError(t, err)
				body = req.EventGatewayConsumePolicyCreate
				_, err = decoded.ToUpdateEventGatewayVirtualClusterConsumePolicyRequest(t.Context(), cl)
				require.NoError(t, err)
			}
			data, err := json.Marshal(body)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			require.Equal(t, "parent-id", payload["parent_policy_id"])
			require.NotContains(t, payload, "parent_policy_ref")
		})
	}
}

func TestEventGatewayParentPolicyResolution(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	for _, side := range []string{"produce", "consume"} {
		t.Run(side, func(t *testing.T) {
			for _, tc := range []struct {
				name       string
				parentType string
				cluster    string
				id         string
				namespace  string
				deleting   bool
				existing   bool
				missing    bool
				mixedRefs  bool
				parentVCID string
				childVCID  string
				self       bool
				wantError  string
			}{
				{name: "ready", parentType: "schemaValidation", cluster: "vc", id: "parent-id"},
				{name: "not programmed", parentType: "schemaValidation", cluster: "vc", wantError: "not programmed"},
				{name: "missing", missing: true, wantError: "not found"},
				{name: "different cluster", parentType: "schemaValidation", cluster: "other", id: "parent-id", wantError: "different EventGatewayVirtualCluster"},
				{name: "wrong type", parentType: "modifyHeaders", cluster: "vc", id: "parent-id", wantError: "must have apiSpec.type schemaValidation"},
				{name: "different namespace", namespace: "other", wantError: "cross-namespace"},
				{name: "deleting parent", parentType: "schemaValidation", cluster: "vc", id: "parent-id", deleting: true, wantError: "being deleted"},
				{name: "existing child may sync", parentType: "schemaValidation", cluster: "vc", id: "parent-id", deleting: true, existing: true},
				{name: "different cluster with mixed reference types", parentType: "schemaValidation", cluster: "vc", id: "parent-id", mixedRefs: true, parentVCID: "other-vc-id", childVCID: "vc-id", wantError: "different EventGatewayVirtualCluster"},
				{name: "same cluster with mixed reference types", parentType: "schemaValidation", cluster: "vc", id: "parent-id", mixedRefs: true, parentVCID: "vc-id", childVCID: "vc-id"},
				{name: "self reference", parentType: "schemaValidation", cluster: "vc", id: "parent-id", self: true, wantError: "must not reference itself"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					meta := metav1.ObjectMeta{Name: "parent", Namespace: "default"}
					if tc.self {
						meta.Name = "child"
					}
					if tc.deleting {
						meta.DeletionTimestamp = new(metav1.Now())
						meta.Finalizers = []string{"test"}
					}
					var parent client.Object
					var resolve func(client.Client) error
					if side == "produce" {
						p := &EventGatewayVirtualClusterProducePolicy{
							ObjectMeta: meta,
							Spec: EventGatewayVirtualClusterProducePolicySpec{
								EventGatewayVirtualClusterRef: policyVirtualClusterRef(tc.cluster),
								APISpec: EventGatewayVirtualClusterProducePolicyAPISpec{
									EventGatewayVirtualClusterProducePolicyConfig: &EventGatewayVirtualClusterProducePolicyConfig{Type: EventGatewayVirtualClusterProducePolicyConfigType(tc.parentType)},
								},
							},
						}
						p.SetKonnectID(tc.id)
						p.SetVirtualClusterID(tc.parentVCID)
						if tc.mixedRefs {
							p.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeKonnectID, KonnectID: new(tc.parentVCID)}
						}
						child := p.DeepCopy()
						child.ObjectMeta = metav1.ObjectMeta{Name: "child", Namespace: "default"}
						child.Spec.EventGatewayVirtualClusterRef = policyVirtualClusterRef("vc")
						child.Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterProducePolicyRef{Name: meta.Name, Namespace: tc.namespace}
						child.SetKonnectID("")
						child.SetVirtualClusterID(tc.childVCID)
						if tc.existing {
							child.SetKonnectID("child-id")
						}
						parent = p
						resolve = func(cl client.Client) error { return child.ResolveKonnectReferences(t.Context(), cl) }
					} else {
						p := &EventGatewayVirtualClusterConsumePolicy{
							ObjectMeta: meta,
							Spec: EventGatewayVirtualClusterConsumePolicySpec{
								EventGatewayVirtualClusterRef: policyVirtualClusterRef(tc.cluster),
								APISpec: EventGatewayVirtualClusterConsumePolicyAPISpec{
									EventGatewayVirtualClusterConsumePolicyConfig: &EventGatewayVirtualClusterConsumePolicyConfig{Type: EventGatewayVirtualClusterConsumePolicyConfigType(tc.parentType)},
								},
							},
						}
						p.SetKonnectID(tc.id)
						p.SetVirtualClusterID(tc.parentVCID)
						if tc.mixedRefs {
							p.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeKonnectID, KonnectID: new(tc.parentVCID)}
						}
						child := p.DeepCopy()
						child.ObjectMeta = metav1.ObjectMeta{Name: "child", Namespace: "default"}
						child.Spec.EventGatewayVirtualClusterRef = policyVirtualClusterRef("vc")
						child.Spec.APISpec.ParentPolicyRef = EventGatewayVirtualClusterConsumePolicyRef{Name: meta.Name, Namespace: tc.namespace}
						child.SetKonnectID("")
						child.SetVirtualClusterID(tc.childVCID)
						if tc.existing {
							child.SetKonnectID("child-id")
						}
						parent = p
						resolve = func(cl client.Client) error { return child.ResolveKonnectReferences(t.Context(), cl) }
					}
					builder := fake.NewClientBuilder().WithScheme(scheme)
					if !tc.missing {
						builder.WithObjects(parent)
					}
					err := resolve(builder.Build())
					if tc.wantError != "" {
						require.ErrorContains(t, err, tc.wantError)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}
