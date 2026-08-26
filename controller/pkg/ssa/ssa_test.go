/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ssa

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/rest"
	kubespec3 "k8s.io/kube-openapi/pkg/spec3"
	validationspec "k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const testFieldManager = "ssa-unit-tests"

func svcWithPort(port int32) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "svc",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "svc"},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       port,
				TargetPort: intstr.FromInt32(port),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func Test_gvToPathKey(t *testing.T) {
	tests := []struct {
		name string
		gv   schema.GroupVersion
		want string
	}{
		{
			name: "core group version path",
			gv:   schema.GroupVersion{Group: "", Version: "v1"},
			want: "api/v1",
		},
		{
			name: "named group version path",
			gv:   schema.GroupVersion{Group: "apps", Version: "v1"},
			want: "apis/apps/v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, gvToPathKey(tc.gv))
		})
	}
}

func Test_fetchBuiltinSchemas_error(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *rest.Config
		groupVersions   []schema.GroupVersion
		wantErrContains string
	}{
		{
			name:            "openapi path listing fails",
			cfg:             &rest.Config{Host: "http://127.0.0.1:0"},
			groupVersions:   []schema.GroupVersion{{Group: "", Version: "v1"}},
			wantErrContains: "failed to list OpenAPI paths",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fetchBuiltinSchemas(tc.cfg, tc.groupVersions)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrContains)
		})
	}
}

func Test_toUnstructuredWithoutStatus(t *testing.T) {
	tests := []struct {
		name      string
		obj       client.Object
		assertOut func(t *testing.T, in client.Object, out *unstructured.Unstructured)
	}{
		{
			name: "typed object",
			obj:  svcWithPort(80),
			assertOut: func(t *testing.T, _ client.Object, out *unstructured.Unstructured) {
				t.Helper()
				_, hasStatus := out.Object["status"]
				assert.False(t, hasStatus)
				assert.Equal(t, "svc", out.GetName())
			},
		},
		{
			name: "unstructured deep-copy",
			obj: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata":   map[string]any{"name": "svc", "namespace": "ns"},
				"status":     map[string]any{"dummy": true},
			}},
			assertOut: func(t *testing.T, in client.Object, out *unstructured.Unstructured) {
				t.Helper()
				_, hasStatus := out.Object["status"]
				assert.False(t, hasStatus)
				u, ok := in.(*unstructured.Unstructured)
				require.True(t, ok)
				_, originalHasStatus := u.Object["status"]
				assert.True(t, originalHasStatus)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := toUnstructuredWithoutStatus(tc.obj)
			require.NoError(t, err)
			tc.assertOut(t, tc.obj, out)
		})
	}
}

func Test_ownedFieldSetForSubresource(t *testing.T) {

	tests := []struct {
		name            string
		obj             client.Object
		manager         string
		subresource     string
		wantErrContains string
	}{
		{
			name: "valid entry",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:     testFieldManager,
					Operation:   metav1.ManagedFieldsOperationApply,
					Subresource: "status",
					FieldsV1:    FieldsWithRawBytes([]byte("{}")),
				}})
				return u
			}(),
			manager:     testFieldManager,
			subresource: "status",
		},
		{
			name: "missing entry returns empty set",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:     testFieldManager,
					Operation:   metav1.ManagedFieldsOperationApply,
					Subresource: "status",
					FieldsV1:    FieldsWithRawBytes([]byte("{}")),
				}})
				return u
			}(),
			manager:     "other-manager",
			subresource: "status",
		},
		{
			name: "malformed fieldsv1 returns error",
			obj: &corev1.Service{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{{
				Manager:     testFieldManager,
				Operation:   metav1.ManagedFieldsOperationApply,
				Subresource: "status",
				FieldsV1:    FieldsWithRawBytes([]byte("{not-json}")),
			}}}},
			manager:         testFieldManager,
			subresource:     "status",
			wantErrContains: "failed to decode managed fields",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set, err := ownedFieldSetForSubresource(tc.obj, tc.manager, tc.subresource)
			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, set)
		})
	}
}

func Test_MergeObjects(t *testing.T) {
	tc := newRealSchemaTypeConverter()
	base := kongServiceWithHost("old.example.com")
	_ = unstructured.SetNestedField(base.Object, "/base-path", "spec", "path")
	overlay := kongServiceWithHost("new.example.com")

	merged, err := MergeObjects(tc, base, overlay)
	require.NoError(t, err)

	// overlay wins on conflict.
	host, found, err := unstructured.NestedString(merged.Object, "spec", "host")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "new.example.com", host)

	// field only set on base is preserved.
	path, found, err := unstructured.NestedString(merged.Object, "spec", "path")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/base-path", path)

	// field set identically on both sides survives the merge.
	port, found, err := unstructured.NestedInt64(merged.Object, "spec", "port")
	require.NoError(t, err)
	require.True(t, found)
	assert.EqualValues(t, 80, port)
}

// Test_ApplyIfChanged uses a real, CRD-derived OpenAPI schema TypeConverter
// (via newRealSchemaTypeConverter) rather than managedfields.NewDeducedTypeConverter.
//
// This distinction matters: NewDeducedTypeConverter cannot correctly
// ExtractItems/Compare struct-typed fields in this codebase's usage - even
// byte-identical objects come back with every field reported as "Added",
// making comparison.IsSame() always false regardless of whether ownership was
// previously claimed. A real schema-based TypeConverter (as production uses,
// see NewTypeConverterProvider) does not have this limitation.
func Test_ApplyIfChanged(t *testing.T) {
	scheme := managerscheme.Get()
	tc := newRealSchemaTypeConverter()

	tests := []struct {
		name      string
		objects   []client.Object
		desired   *unstructured.Unstructured
		build     func(client.WithWatch) client.Client
		repeat    int
		wantRes   op.Result
		wantErr   bool
		verifyObj bool
	}{
		{
			name:      "create on not found",
			objects:   nil,
			desired:   kongServiceWithHost("example.com"),
			build:     func(c client.WithWatch) client.Client { return c },
			wantRes:   op.Created,
			verifyObj: true,
		},
		{
			name:    "updated when spec changes",
			objects: []client.Object{kongServiceWithHost("old.example.com")},
			desired: kongServiceWithHost("new.example.com"),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Updated,
		},
		{
			name: "noop when field manager already owns matching values",
			objects: []client.Object{func() client.Object {
				u := kongServiceWithHost("example.com")
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:    testFieldManager,
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: kongServiceGVK.GroupVersion().String(),
					FieldsType: "FieldsV1",
					FieldsV1:   FieldsWithRawBytes(ownedFieldsRaw(t, tc, u)),
				}})
				return u
			}()},
			desired: kongServiceWithHost("example.com"),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Noop,
		},
		{
			name: "noop for matching preserve-unknown object fields",
			objects: []client.Object{func() client.Object {
				u := kongPluginWithHeaders([]string{"X-Test:true"})
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:    testFieldManager,
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: kongPluginGVK.GroupVersion().String(),
					FieldsType: "FieldsV1",
					FieldsV1:   FieldsWithRawBytes(ownedFieldsRaw(t, tc, u)),
				}})
				return u
			}()},
			desired: kongPluginWithHeaders([]string{"X-Test:true"}),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Noop,
		},
		{
			name: "updated when preserve-unknown object field changes",
			objects: []client.Object{func() client.Object {
				u := kongPluginWithHeaders([]string{"X-Test:true"})
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:    testFieldManager,
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: kongPluginGVK.GroupVersion().String(),
					FieldsType: "FieldsV1",
					FieldsV1:   FieldsWithRawBytes(ownedFieldsRaw(t, tc, u)),
				}})
				return u
			}()},
			desired: kongPluginWithHeaders([]string{"X-Test:false"}),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
						return nil
					},
				})
			},
			wantRes: op.Updated,
		},
		{
			name: "updated when preserve-unknown object field is removed",
			objects: []client.Object{func() client.Object {
				u := kongPluginWithHeaders([]string{"X-Test:true"})
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:    testFieldManager,
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: kongPluginGVK.GroupVersion().String(),
					FieldsType: "FieldsV1",
					FieldsV1:   FieldsWithRawBytes(ownedFieldsRaw(t, tc, u)),
				}})
				return u
			}()},
			desired: kongPluginWithHeaders(nil),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					Apply: func(context.Context, client.WithWatch, runtime.ApplyConfiguration, ...client.ApplyOption) error {
						return nil
					},
				})
			},
			wantRes: op.Updated,
		},
		{
			// Regression test: even when the live values already match desired, if
			// fieldManager has no managed-fields entry yet on the existing object
			// (e.g. it was created/owned by a different manager), ApplyIfChanged
			// must still issue an apply so that ownership of the relevant fields is
			// claimed for fieldManager. Otherwise the object would never gain a
			// managed-fields entry for fieldManager until a real value changes,
			// leaving SSA conflict detection ineffective for it in the meantime.
			name:    "claims ownership when values match but no managed-fields entry exists for our manager",
			objects: []client.Object{kongServiceWithHost("example.com")},
			desired: kongServiceWithHost("example.com"),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Updated,
		},
		{
			name:    "get error propagated",
			objects: []client.Object{kongServiceWithHost("old.example.com")},
			desired: kongServiceWithHost("new.example.com"),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return assert.AnError
					},
				})
			},
			wantRes: op.Noop,
			wantErr: true,
		},
		{
			name:    "apply create error returned with created result",
			objects: nil,
			desired: kongServiceWithHost("example.com"),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					Apply: func(ctx context.Context, cl client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantRes: op.Created,
			wantErr: true,
		},
	}

	for _, tcse := range tests {
		t.Run(tcse.name, func(t *testing.T) {
			base := fake.NewClientBuilder().WithScheme(scheme).WithReturnManagedFields().WithObjects(tcse.objects...).Build()
			cl := tcse.build(base)
			repeat := tcse.repeat
			if repeat == 0 {
				repeat = 1
			}

			var res op.Result
			var err error
			for range repeat {
				res, err = ApplyIfChanged(t.Context(), logr.Discard(), cl, tc, tcse.desired, testFieldManager)
			}
			assert.Equal(t, tcse.wantRes, res)
			if tcse.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tcse.verifyObj && !tcse.wantErr {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(kongServiceGVK)
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(tcse.desired), got))
				wantHost, _, _ := unstructured.NestedString(tcse.desired.Object, "spec", "host")
				gotHost, _, _ := unstructured.NestedString(got.Object, "spec", "host")
				assert.Equal(t, wantHost, gotHost)
			}
		})
	}
}

func Test_ApplyStatusIfChanged(t *testing.T) {
	scheme := managerscheme.Get()
	tc := newRealSchemaTypeConverter()

	tests := []struct {
		name    string
		objects []client.Object
		desired *unstructured.Unstructured
		build   func(client.WithWatch) client.Client
		repeat  int
		wantRes op.Result
		wantErr bool
	}{
		{
			name:    "not found returns error",
			objects: nil,
			desired: kongServiceWithCondition(metav1.ConditionTrue),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Noop,
			wantErr: true,
		},
		{
			name:    "updated when status changes",
			objects: []client.Object{kongServiceWithCondition(metav1.ConditionFalse)},
			desired: kongServiceWithCondition(metav1.ConditionTrue),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Updated,
		},
		{
			name:    "get error propagated",
			objects: []client.Object{kongServiceWithCondition(metav1.ConditionFalse)},
			desired: kongServiceWithCondition(metav1.ConditionTrue),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return assert.AnError
					},
				})
			},
			wantRes: op.Noop,
			wantErr: true,
		},
		{
			name:    "status apply error returned with updated result",
			objects: []client.Object{kongServiceWithCondition(metav1.ConditionFalse)},
			desired: kongServiceWithCondition(metav1.ConditionTrue),
			build: func(c client.WithWatch) client.Client {
				return interceptor.NewClient(c, interceptor.Funcs{
					SubResourceApply: func(ctx context.Context, cl client.Client, subResourceName string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantRes: op.Updated,
			wantErr: true,
		},
	}

	for _, tcse := range tests {
		t.Run(tcse.name, func(t *testing.T) {
			b := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tcse.objects...)
			if len(tcse.objects) > 0 {
				b = b.WithStatusSubresource(tcse.objects...)
			}
			base := b.Build()
			cl := tcse.build(base)

			repeat := tcse.repeat
			if repeat == 0 {
				repeat = 1
			}

			var res op.Result
			var err error
			for range repeat {
				res, err = ApplyStatusIfChanged(t.Context(), logr.Discard(), cl, tc, tcse.desired, testFieldManager)
			}
			assert.Equal(t, tcse.wantRes, res)
			if tcse.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// kongServiceGVK is the GVK of the CRD used to build the real, schema-based
// TypeConverter shared by this file's tests.
var (
	kongServiceGVK = schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
	kongPluginGVK  = schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1", Kind: "KongPlugin"}
)

// realSchemaCRDManifests lists the real, repo-checked-in CRD manifest used to
// build a schema-based TypeConverter, following the same approach as
// controller/hybridgateway's newTestTypeConverter.
var realSchemaCRDManifests = []string{
	"configuration.konghq.com_kongplugins.yaml",
	"configuration.konghq.com_kongservices.yaml",
}

// newRealSchemaTypeConverter is memoized (via [sync.OnceValue]) since it is
// expensive to build (reads + parses CRD manifests, builds OpenAPI schemas)
// and is read-only/safe to share across tests.
var newRealSchemaTypeConverter = sync.OnceValue(func() managedfields.TypeConverter {
	var specs []*kubespec3.OpenAPI
	for _, file := range realSchemaCRDManifests {
		path := filepath.Join("..", "..", "..", "config", "crd", "kong-operator", file)
		raw, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Errorf("failed to read CRD manifest %s: %w", path, err))
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := k8syaml.Unmarshal(raw, crd); err != nil {
			panic(fmt.Errorf("failed to unmarshal CRD manifest %s: %w", path, err))
		}

		for _, v := range crd.Spec.Versions {
			spec, err := aexbuilder.BuildOpenAPIV3(crd, v.Name, aexbuilder.Options{})
			if err != nil {
				panic(fmt.Errorf("failed to build OpenAPI v3 for %s/%s: %w", crd.Name, v.Name, err))
			}
			specs = append(specs, spec)
		}
	}

	merged, err := aexbuilder.MergeSpecsV3(specs...)
	if err != nil {
		panic(fmt.Errorf("failed to merge CRD OpenAPI v3 specs: %w", err))
	}

	schemas := map[string]*validationspec.Schema{}
	if merged.Components != nil {
		maps.Copy(schemas, merged.Components.Schemas)
	}

	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		panic(fmt.Errorf("failed to create TypeConverter: %w", err))
	}
	return tc
})

// kongServiceWithHost returns a KongService named "svc" with the given host.
func kongServiceWithHost(host string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kongServiceGVK)
	u.SetNamespace("ns")
	u.SetName("svc")
	_ = unstructured.SetNestedField(u.Object, host, "spec", "host")
	_ = unstructured.SetNestedField(u.Object, int64(80), "spec", "port")
	_ = unstructured.SetNestedField(u.Object, "http", "spec", "protocol")
	return u
}

// kongPluginWithHeaders returns a KongPlugin whose nested config field uses
// the preserve-unknown schema from the real CRD. A nil headers slice omits
// config to exercise removal of an owned preserve-unknown field.
func kongPluginWithHeaders(headers []string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kongPluginGVK)
	u.SetNamespace("ns")
	u.SetName("plugin")
	_ = unstructured.SetNestedField(u.Object, "request-transformer", "plugin")
	if headers != nil {
		_ = unstructured.SetNestedStringSlice(u.Object, headers, "config", "add", "headers")
	}
	return u
}

// kongServiceWithCondition returns a KongService named "svc" with a single
// "Programmed" status condition, for exercising ApplyStatusIfChanged.
func kongServiceWithCondition(status metav1.ConditionStatus) *unstructured.Unstructured {
	u := kongServiceWithHost("example.com")
	cond := map[string]any{
		"type":               "Programmed",
		"status":             string(status),
		"reason":             "Programmed",
		"message":            "",
		"lastTransitionTime": "1970-01-01T00:00:00Z",
	}
	_ = unstructured.SetNestedSlice(u.Object, []any{cond}, "status", "conditions")
	return u
}

// ownedFieldsRaw computes the FieldsV1 raw JSON that a real Server-Side
// Apply of obj by fieldManager would have recorded, by converting obj to its
// field set via tc. Used to build realistic ManagedFieldsEntry fixtures.
func ownedFieldsRaw(t *testing.T, tc managedfields.TypeConverter, obj *unstructured.Unstructured) []byte {
	t.Helper()
	typed, err := tc.ObjectToTyped(obj)
	require.NoError(t, err)
	set, err := typed.ToFieldSet()
	require.NoError(t, err)
	raw, err := set.ToJSON()
	require.NoError(t, err)
	return raw
}
