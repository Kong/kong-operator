package objects

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestDeleteAll(t *testing.T) {
	scheme := scheme.Get()

	newCM := func(name string) corev1.ConfigMap {
		return corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
		}
	}
	deletingCM := func(name string) corev1.ConfigMap {
		cm := newCM(name)
		cm.DeletionTimestamp = new(metav1.NewTime(time.Now()))
		cm.Finalizers = []string{"test-finalizer"}
		return cm
	}

	tests := []struct {
		name          string
		objs          []corev1.ConfigMap
		interceptor   interceptor.Funcs
		wantDeleted   bool
		wantErrSubstr string
	}{
		{
			name:        "empty slice",
			objs:        []corev1.ConfigMap{},
			wantDeleted: false,
		},
		{
			name:        "single object deleted",
			objs:        []corev1.ConfigMap{newCM("cm-1")},
			wantDeleted: true,
		},
		{
			name:        "multiple objects all deleted",
			objs:        []corev1.ConfigMap{newCM("cm-1"), newCM("cm-2"), newCM("cm-3")},
			wantDeleted: true,
		},
		{
			name:        "object with deletion timestamp skipped",
			objs:        []corev1.ConfigMap{deletingCM("cm-1")},
			wantDeleted: false,
		},
		{
			name:        "mix deleting and active",
			objs:        []corev1.ConfigMap{deletingCM("cm-1"), newCM("cm-2")},
			wantDeleted: true,
		},
		{
			name: "not-found error treated as deleted",
			objs: []corev1.ConfigMap{newCM("cm-1")},
			interceptor: interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, obj.GetName())
				},
			},
			wantDeleted: true,
		},
		{
			name: "delete error propagated",
			objs: []corev1.ConfigMap{newCM("cm-1")},
			interceptor: interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("internal error")
				},
			},
			wantDeleted:   false,
			wantErrSubstr: "failed to delete some objects",
		},
		{
			name: "partial failure returns deleted true and error",
			objs: []corev1.ConfigMap{newCM("cm-1"), newCM("cm-2")},
			interceptor: interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					if obj.GetName() == "cm-1" {
						return fmt.Errorf("internal error")
					}
					return nil
				},
			},
			wantDeleted:   true,
			wantErrSubstr: "failed to delete some objects",
		},
		{
			name: "all fail returns deleted false and error",
			objs: []corev1.ConfigMap{newCM("cm-1"), newCM("cm-2")},
			interceptor: interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("internal error")
				},
			},
			wantDeleted:   false,
			wantErrSubstr: "failed to delete some objects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existingObjs := make([]client.Object, 0, len(tt.objs))
			for i := range tt.objs {
				existingObjs = append(existingObjs, &tt.objs[i])
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingObjs...).
				WithInterceptorFuncs(tt.interceptor).
				Build()

			got, err := DeleteAll(t.Context(), cl, tt.objs)

			require.Equal(t, tt.wantDeleted, got)
			if tt.wantErrSubstr != "" {
				require.ErrorContains(t, err, tt.wantErrSubstr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
