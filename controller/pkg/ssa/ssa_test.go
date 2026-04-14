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
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const testFieldManager = "ssa-unit-tests"

type fakeManagerWithConfig struct {
	ctrl.Manager

	config *rest.Config
}

func (f fakeManagerWithConfig) GetConfig() *rest.Config {
	return f.config
}

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

func depWithReady(ready int32) *appsv1.Deployment {
	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "dp",
		},
	}
	d.Status.Replicas = 1
	d.Status.ReadyReplicas = ready
	return d
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

func Test_NewTypeConverter_error(t *testing.T) {
	tests := []struct {
		name            string
		mgr             ctrl.Manager
		groupVersions   []schema.GroupVersion
		wantErrContains string
	}{
		{
			name:            "openapi path listing fails",
			mgr:             fakeManagerWithConfig{config: &rest.Config{Host: "http://127.0.0.1:0"}},
			groupVersions:   []schema.GroupVersion{{Group: "", Version: "v1"}},
			wantErrContains: "failed to list OpenAPI paths",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTypeConverter(tc.mgr, tc.groupVersions)
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
					FieldsV1:    &metav1.FieldsV1{Raw: []byte("{}")},
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
					FieldsV1:    &metav1.FieldsV1{Raw: []byte("{}")},
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
				FieldsV1:    &metav1.FieldsV1{Raw: []byte("{not-json")},
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
	tc := managedfields.NewDeducedTypeConverter()
	base := svcWithPort(80)
	overlay := svcWithPort(90)
	overlay.Spec.Selector["extra"] = "true"

	merged, err := MergeObjects(tc, base, overlay)
	require.NoError(t, err)

	ports, found, err := unstructured.NestedSlice(merged.Object, "spec", "ports")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, ports, 1)
	port0 := ports[0].(map[string]any)
	assert.EqualValues(t, int64(90), port0["port"])

	selector, found, err := unstructured.NestedStringMap(merged.Object, "spec", "selector")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "true", selector["extra"])
}

func Test_ApplyIfChanged(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()

	tests := []struct {
		name      string
		objects   []client.Object
		desired   *corev1.Service
		build     func(client.WithWatch) client.Client
		repeat    int
		wantRes   op.Result
		wantErr   bool
		verifyObj bool
	}{
		{
			name:      "create on not found",
			objects:   nil,
			desired:   svcWithPort(80),
			build:     func(c client.WithWatch) client.Client { return c },
			wantRes:   op.Created,
			verifyObj: true,
		},
		{
			name:    "updated when spec changes",
			objects: []client.Object{svcWithPort(80)},
			desired: svcWithPort(90),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Updated,
		},
		{
			name:    "get error propagated",
			objects: []client.Object{svcWithPort(80)},
			desired: svcWithPort(90),
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
			desired: svcWithPort(80),
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
			base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tcse.objects...).Build()
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
				got := &corev1.Service{}
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(tcse.desired), got))
				assert.Equal(t, tcse.desired.Spec.Ports[0].Port, got.Spec.Ports[0].Port)
			}
		})
	}
}

func Test_ApplyStatusIfChanged(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()

	tests := []struct {
		name    string
		objects []client.Object
		desired *appsv1.Deployment
		build   func(client.WithWatch) client.Client
		repeat  int
		wantRes op.Result
		wantErr bool
	}{
		{
			name:    "not found returns error",
			objects: nil,
			desired: depWithReady(1),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Noop,
			wantErr: true,
		},
		{
			name:    "updated when status changes",
			objects: []client.Object{depWithReady(0)},
			desired: depWithReady(1),
			build:   func(c client.WithWatch) client.Client { return c },
			wantRes: op.Updated,
		},
		{
			name:    "get error propagated",
			objects: []client.Object{depWithReady(0)},
			desired: depWithReady(1),
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
			objects: []client.Object{depWithReady(0)},
			desired: depWithReady(1),
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
