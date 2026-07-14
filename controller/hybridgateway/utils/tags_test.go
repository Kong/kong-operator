package utils

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// tagsSvc builds a Service in the "ns" namespace used throughout this test.
func tagsSvc(name string, anns map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Annotations: anns}}
}

// tagsSvcIn builds a Service in an arbitrary namespace, for cases exercising
// the per-ref namespace override.
func tagsSvcIn(namespace, name string, anns map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: anns}}
}

func backendRef(name string) gwtypes.BackendRef {
	return gwtypes.BackendRef{BackendObjectReference: gwtypes.BackendObjectReference{Name: gwtypes.ObjectName(name)}}
}

// backendRefIn builds a BackendRef with an explicit namespace override.
func backendRefIn(name, namespace string) gwtypes.BackendRef {
	ns := gwtypes.Namespace(namespace)
	return gwtypes.BackendRef{BackendObjectReference: gwtypes.BackendObjectReference{
		Name:      gwtypes.ObjectName(name),
		Namespace: &ns,
	}}
}

func TestTagsFromBackendRefs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name     string
		objects  []runtime.Object
		refs     []gwtypes.BackendRef
		expected []string
	}{
		{
			name:     "no refs",
			refs:     nil,
			expected: nil,
		},
		{
			name:     "service without annotation",
			objects:  []runtime.Object{tagsSvc("svc-a", nil)},
			refs:     []gwtypes.BackendRef{backendRef("svc-a")},
			expected: nil,
		},
		{
			name:     "first service with tags wins",
			objects:  []runtime.Object{tagsSvc("svc-a", map[string]string{"konghq.com/tags": "a,b"}), tagsSvc("svc-b", map[string]string{"konghq.com/tags": "c"})},
			refs:     []gwtypes.BackendRef{backendRef("svc-a"), backendRef("svc-b")},
			expected: []string{"a", "b"},
		},
		{
			name:     "skips untagged then finds tagged",
			objects:  []runtime.Object{tagsSvc("svc-a", nil), tagsSvc("svc-b", map[string]string{"konghq.com/tags": "c"})},
			refs:     []gwtypes.BackendRef{backendRef("svc-a"), backendRef("svc-b")},
			expected: []string{"c"},
		},
		{
			name:     "missing service is skipped",
			objects:  nil,
			refs:     []gwtypes.BackendRef{backendRef("ghost")},
			expected: nil,
		},
		{
			name:     "ref namespace override resolves service in other namespace",
			objects:  []runtime.Object{tagsSvcIn("other-ns", "svc-a", map[string]string{"konghq.com/tags": "x"})},
			refs:     []gwtypes.BackendRef{backendRefIn("svc-a", "other-ns")},
			expected: []string{"x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.objects...).Build()
			got := TagsFromBackendRefs(context.Background(), cl, "ns", tc.refs, logr.Discard())
			assert.Equal(t, tc.expected, got)
		})
	}
}
