package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

func TestKegDataPlaneKonnectNamespacedRef(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KegDataPlane object",
			input:    &corev1.ConfigMap{},
			expected: nil,
		},
		{
			name: "returns nil when KonnectNamespacedRef is not set",
			input: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec:       eventgatewayv1alpha1.KegDataPlaneSpec{},
			},
			expected: nil,
		},
		{
			name: "returns namespace/name when KonnectNamespacedRef is set",
			input: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
						KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
							Name: "my-gateway",
						},
					},
				},
			},
			expected: []string{"default/my-gateway"},
		},
		{
			name: "uses the KegDataPlane namespace, not a separate one",
			input: &eventgatewayv1alpha1.KegDataPlane{
				ObjectMeta: metav1.ObjectMeta{Namespace: "other-ns"},
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
						KonnectNamespacedRef: &eventgatewayv1alpha1.KonnectNamespacedRef{
							Name: "my-gateway",
						},
					},
				},
			},
			expected: []string{"other-ns/my-gateway"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kegDataPlaneKonnectNamespacedRef(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOptionsForKegDataPlane(t *testing.T) {
	options := OptionsForKegDataPlane()
	require.Len(t, options, 1)
	opt := options[0]
	require.IsType(t, &eventgatewayv1alpha1.KegDataPlane{}, opt.Object)
	require.Equal(t, IndexFieldKegDataPlaneOnKonnectEventGateway, opt.Field)
	require.NotNil(t, opt.ExtractValueFn)
}
