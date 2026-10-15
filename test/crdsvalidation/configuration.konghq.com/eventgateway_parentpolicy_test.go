package configuration_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
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
			t.Run("apiSpec can be omitted", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", false)
				unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec")
				obj.SetFinalizers([]string{"test.konghq.com/cleanup"})
				require.NoError(t, cl.Create(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedField(obj.Object, "policy-id", "status", "id"))
				require.NoError(t, cl.Status().Update(t.Context(), obj))
				obj.SetLabels(map[string]string{"updated": "true"})
				require.NoError(t, cl.Update(t.Context(), obj))
				require.NoError(t, cl.Delete(t.Context(), obj))
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
				old := obj.DeepCopy()
				obj.SetFinalizers(nil)
				require.NoError(t, cl.Patch(t.Context(), obj, client.MergeFrom(old)))
				require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)))
			})
			t.Run("empty apiSpec has standard type validation", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", false)
				require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{}, "spec", "apiSpec"))
				err := cl.Create(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "spec.apiSpec.type: Required value")
				require.NotContains(t, err.Error(), "no such key")
			})
			t.Run("cannot add a parent to a programmed policy without apiSpec", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", true)
				apiSpec, _, err := unstructured.NestedMap(obj.Object, "spec", "apiSpec")
				require.NoError(t, err)
				unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec")
				require.NoError(t, cl.Create(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedField(obj.Object, "policy-id", "status", "id"))
				require.NoError(t, cl.Status().Update(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedMap(obj.Object, apiSpec, "spec", "apiSpec"))
				err = cl.Update(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is immutable")
			})
			t.Run("cannot remove apiSpec containing a programmed parent reference", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", true)
				require.NoError(t, cl.Create(t.Context(), obj))
				require.NoError(t, unstructured.SetNestedField(obj.Object, "policy-id", "status", "id"))
				require.NoError(t, cl.Status().Update(t.Context(), obj))
				unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec")
				err := cl.Update(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is immutable")
			})
			t.Run("changing to a field policy still requires a parent", func(t *testing.T) {
				obj := newPolicy("modifyHeaders", false)
				require.NoError(t, cl.Create(t.Context(), obj))
				field := newPolicy(side.fieldType, false)
				apiSpec, _, err := unstructured.NestedMap(field.Object, "spec", "apiSpec")
				require.NoError(t, err)
				require.NoError(t, unstructured.SetNestedMap(obj.Object, apiSpec, "spec", "apiSpec"))
				err = cl.Update(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is required")
			})
			t.Run("legacy parent-less policies survive a CRD upgrade", func(t *testing.T) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				mapping, err := cl.RESTMapper().RESTMapping(schema.GroupKind{
					Group: configurationv1alpha1.GroupVersion.Group, Kind: side.kind,
				}, configurationv1alpha1.GroupVersion.Version)
				require.NoError(t, err)
				crdName := mapping.Resource.GroupResource().String()
				require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: crdName}, crd))
				currentSpec := crd.Spec.DeepCopy()
				for i := range crd.Spec.Versions {
					schema := crd.Spec.Versions[i].Schema.OpenAPIV3Schema
					schema.XValidations = slices.DeleteFunc(schema.XValidations, func(rule apiextensionsv1.ValidationRule) bool {
						return strings.Contains(rule.Rule, "parentPolicyRef")
					})
				}
				require.NoError(t, cl.Update(t.Context(), crd))
				require.EventuallyWithT(t, func(ct *assert.CollectT) {
					assert.NoError(ct, cl.Create(t.Context(), newPolicy(side.fieldType, false), client.DryRunAll))
				}, consts.WaitTime, consts.TickTime)
				legacy := newPolicy(side.fieldType, false)
				legacy.SetFinalizers([]string{"test.konghq.com/cleanup"})
				require.NoError(t, cl.Create(t.Context(), legacy))
				repair := newPolicy(side.fieldType, false)
				require.NoError(t, cl.Create(t.Context(), repair))

				require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: crdName}, crd))
				crd.Spec = *currentSpec
				require.NoError(t, cl.Update(t.Context(), crd))
				require.EventuallyWithT(t, func(ct *assert.CollectT) {
					err := cl.Create(t.Context(), newPolicy(side.fieldType, false), client.DryRunAll)
					assert.True(ct, apierrors.IsInvalid(err), "%v", err)
				}, consts.WaitTime, consts.TickTime)

				legacy.SetLabels(map[string]string{"updated": "true"})
				require.NoError(t, cl.Update(t.Context(), legacy))
				require.NoError(t, unstructured.SetNestedSlice(legacy.Object, []any{}, "status", "conditions"))
				require.NoError(t, cl.Status().Update(t.Context(), legacy))
				require.NoError(t, cl.Delete(t.Context(), legacy))
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(legacy), legacy))
				old := legacy.DeepCopy()
				legacy.SetFinalizers(nil)
				require.NoError(t, cl.Patch(t.Context(), legacy, client.MergeFrom(old)))
				require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(legacy), legacy)))

				require.NoError(t, unstructured.SetNestedMap(repair.Object, map[string]any{"name": "parent"}, "spec", "apiSpec", "parentPolicyRef"))
				require.NoError(t, cl.Update(t.Context(), repair))
				unstructured.RemoveNestedField(repair.Object, "spec", "apiSpec", "parentPolicyRef")
				err = cl.Update(t.Context(), repair)
				require.True(t, apierrors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "parentPolicyRef is required")
			})
		})
	}
}
