package configuration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayParentPolicyValidation(t *testing.T) {
	t.Parallel()
	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	for _, side := range []struct {
		name      string
		kind      string
		fieldType string
		config    string
	}{
		{
			name: "produce", kind: "EventGatewayVirtualClusterProducePolicy", fieldType: "encryptFields",
			config: `{"config":{"failureMode":"reject","encryptFields":[{"paths":{"type":"array","array":[{"match":"secret"}]},"encryptionKey":{"type":"static","static":{"key":{"name":"test-key"}}}}]}}`,
		},
		{
			name: "consume", kind: "EventGatewayVirtualClusterConsumePolicy", fieldType: "decryptFields",
			config: `{"config":{"failureMode":"error","decryptFields":{"paths":{"type":"array","array":[{"match":"secret"}]}},"keySources":[{"type":"static","static":{}}]}}`,
		},
	} {
		t.Run(side.name, func(t *testing.T) {
			newPolicy := func(policyType string, parent bool) *unstructured.Unstructured {
				t.Helper()
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       side.kind,
					"metadata":   map[string]any{"generateName": "policy-", "namespace": ns.Name},
					"spec": map[string]any{
						"eventGatewayVirtualClusterRef": map[string]any{"type": "namespacedRef", "namespacedRef": map[string]any{"name": "vc"}},
						"apiSpec":                       map[string]any{"type": policyType},
					},
				}}
				var config map[string]any
				switch policyType {
				case side.fieldType:
					require.NoError(t, json.Unmarshal([]byte(side.config), &config))
				case "modifyHeaders":
					require.NoError(t, json.Unmarshal([]byte(`{"config":{"actions":[{"op":"set","set":{"key":"x-test","value":"test"}}]}}`), &config))
				default:
					require.NoError(t, json.Unmarshal([]byte(`{"config":{"type":"json","json":{}}}`), &config))
				}
				require.NoError(t, unstructured.SetNestedMap(obj.Object, config, "spec", "apiSpec", policyType))
				if parent {
					require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{"name": "parent"}, "spec", "apiSpec", "parentPolicyRef"))
				}
				return obj
			}
			t.Run("field policy requires a parent", func(t *testing.T) {
				err := cl.Create(t.Context(), newPolicy(side.fieldType, false))
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is required")
				obj := newPolicy(side.fieldType, true)
				require.NoError(t, cl.Create(t.Context(), obj))
				ref, found, err := unstructured.NestedMap(obj.Object, "spec", "apiSpec", "parentPolicyRef")
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, "parent", ref["name"])
				require.Equal(t, side.kind, ref["kind"])
			})
			t.Run("only same-side parents are accepted", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", true)
				require.NoError(t, unstructured.SetNestedField(obj.Object, "KongRoute", "spec", "apiSpec", "parentPolicyRef", "kind"))
				err := cl.Create(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, side.kind)
			})
			t.Run("schema policies cannot have parents", func(t *testing.T) {
				err := cl.Create(t.Context(), newPolicy("schemaValidation", true))
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is only supported")
			})
			for _, change := range []string{"add", "change", "remove"} {
				for _, inKonnect := range []bool{false, true} {
					name := change + " before creation"
					if inKonnect {
						name = change + " after creation"
					}
					t.Run(name, func(t *testing.T) {
						obj := newPolicy("modifyHeaders", change != "add")
						require.NoError(t, cl.Create(t.Context(), obj))
						if inKonnect {
							require.NoError(t, unstructured.SetNestedField(obj.Object, "policy-id", "status", "id"))
							require.NoError(t, cl.Status().Update(t.Context(), obj))
						}
						if change == "remove" {
							unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec", "parentPolicyRef")
						} else {
							require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{"name": "another-parent"}, "spec", "apiSpec", "parentPolicyRef"))
						}
						err := cl.Update(t.Context(), obj)
						if inKonnect {
							require.True(t, apierrors.IsInvalid(err), "%v", err)
							require.ErrorContains(t, err, "parentPolicyRef is immutable")
						} else {
							require.NoError(t, err)
						}
					})
				}
			}
			t.Run("configuration remains editable after creation", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", true)
				require.NoError(t, cl.Create(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedField(obj.Object, "policy-id", "status", "id"))
				require.NoError(t, cl.Status().Update(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedField(obj.Object, "updated", "spec", "apiSpec", "modifyHeaders", "description"))
				require.NoError(t, cl.Update(t.Context(), obj))
			})
		})
	}
}
