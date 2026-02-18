package managedfields

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/generated"
)

// helper functions for pointer values.
func ptrBool(b bool) *bool    { return &b }
func ptrInt64(i int64) *int64 { return &i }
func TestExtractAsUnstructured(t *testing.T) {
	testCases := []struct {
		name        string
		obj         runtime.Object
		wantSpec    map[string]any
		expectError bool
	}{
		{
			name: "typed KongRoute full spec with managed fields",
			obj: func() runtime.Object {
				route := &configurationv1alpha1.KongRoute{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "configuration.konghq.com/v1alpha1",
						Kind:       "KongRoute",
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Hosts:   []string{"example.com", "another.com"},
							Methods: []string{"GET", "POST"},
							Paths:   []string{"/foo", "/bar"},
							Protocols: []sdkkonnectcomp.RouteJSONProtocols{
								sdkkonnectcomp.RouteJSONProtocols("http"),
								sdkkonnectcomp.RouteJSONProtocols("https"),
							},
							StripPath:     ptrBool(true),
							PreserveHost:  ptrBool(false),
							RegexPriority: ptrInt64(10),
						},
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type: "namespacedRef",
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "svc-1",
							},
						},
					},
				}
				// Add managed fields for test-manager
				route.SetManagedFields([]metav1.ManagedFieldsEntry{
					{
						Manager:     "test-manager",
						Operation:   metav1.ManagedFieldsOperationApply,
						Subresource: "",
						FieldsV1:    &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:hosts":{},"f:methods":{},"f:paths":{},"f:protocols":{},"f:strip_path":{},"f:preserve_host":{},"f:regex_priority":{},"f:serviceRef":{"f:type":{},"f:namespacedRef":{"f:name":{}}}}}`)},
					},
				})
				return route
			}(),
			wantSpec: map[string]any{
				"hosts":          []any{"example.com", "another.com"},
				"methods":        []any{"GET", "POST"},
				"paths":          []any{"/foo", "/bar"},
				"protocols":      []any{"http", "https"},
				"strip_path":     true,
				"preserve_host":  false,
				"regex_priority": int64(10),
				"serviceRef": map[string]any{
					"type": "namespacedRef",
					"namespacedRef": map[string]any{
						"name": "svc-1",
					},
				},
			},
			expectError: false,
		},
		{
			name: "typed KongRoute with no managed fields (should return nil)",
			obj: &configurationv1alpha1.KongRoute{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "configuration.konghq.com/v1alpha1",
					Kind:       "KongRoute",
				},
				Spec: configurationv1alpha1.KongRouteSpec{
					KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
						Hosts: []string{"example.com"},
					},
				},
			},
			wantSpec:    nil,
			expectError: false,
		},
		{
			name:        "GetObjectType error",
			obj:         &brokenObject{},
			wantSpec:    nil,
			expectError: true,
		},
		{
			name:        "toTyped error",
			obj:         &brokenUnstructured{},
			wantSpec:    nil,
			expectError: true,
		},
		{
			name:        "apimeta.Accessor error",
			obj:         &noMetaObject{},
			wantSpec:    nil,
			expectError: true,
		},
		{
			name: "missing kind/apiVersion",
			obj: func() runtime.Object {
				route := &configurationv1alpha1.KongRoute{
					Spec: configurationv1alpha1.KongRouteSpec{
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Hosts: []string{"example.com"},
						},
					},
				}
				route.SetManagedFields([]metav1.ManagedFieldsEntry{
					{
						Manager:     "test-manager",
						Operation:   metav1.ManagedFieldsOperationApply,
						Subresource: "",
						FieldsV1:    &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:hosts":{}}}`)},
					},
				})
				return route
			}(),
			wantSpec: map[string]any{
				"hosts": []any{"example.com"},
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			unstructuredObj, err := ExtractAsUnstructured(tc.obj, "test-manager", "")
			if tc.expectError {
				assert.Error(t, err)
				assert.Nil(t, unstructuredObj)
				return
			}
			assert.NoError(t, err)
			if tc.wantSpec == nil {
				assert.Nil(t, unstructuredObj)
				return
			}
			assert.NotNil(t, unstructuredObj)
			spec, ok := unstructuredObj.Object["spec"].(map[string]any)
			assert.True(t, ok)
			assert.Equal(t, tc.wantSpec, spec)
		})
	}
}

func TestFindManagedFields(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{
		{
			Manager:     "foo",
			Operation:   metav1.ManagedFieldsOperationApply,
			Subresource: "bar",
			FieldsV1:    &metav1.FieldsV1{Raw: []byte(`{"f:spec":{}}`)},
		},
	})
	entry, ok := findManagedFields(obj, "foo", "bar")
	assert.True(t, ok)
	assert.Equal(t, "foo", entry.Manager)
	assert.Equal(t, "bar", entry.Subresource)

	_, ok = findManagedFields(obj, "baz", "bar")
	assert.False(t, ok)
}

func TestToTyped(t *testing.T) {
	parser := generated.Parser()
	objectType := parser.Type("com.github.kong.kong-operator.api.configuration.v1alpha1.KongRoute")

	testCases := []struct {
		name string
		obj  runtime.Object
	}{
		{
			name: "unstructured",
			obj: func() *unstructured.Unstructured {
				u := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongRoute",
					"spec":       map[string]any{"hosts": []any{"example.com"}},
				}}
				u.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "configuration.konghq.com",
					Version: "v1alpha1",
					Kind:    "KongRoute",
				})
				return u
			}(),
		},
		{
			name: "typed",
			obj: &configurationv1alpha1.KongRoute{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "configuration.konghq.com/v1alpha1",
					Kind:       "KongRoute",
				},
				Spec: configurationv1alpha1.KongRouteSpec{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val, err := toTyped(tc.obj, objectType)
			assert.NoError(t, err)
			assert.NotNil(t, val)
		})
	}
}
