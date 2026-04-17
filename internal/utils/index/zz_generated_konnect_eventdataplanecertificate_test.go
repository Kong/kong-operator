package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestOptionsForKonnectEventDataPlaneCertificate(t *testing.T) {
	options := OptionsForKonnectEventDataPlaneCertificate()

	require.Len(t, options, 1)

	option := options[0]
	assert.IsType(t, &konnectv1alpha1.KonnectEventDataPlaneCertificate{}, option.Object)
	assert.Equal(t, IndexFieldKonnectEventDataPlaneCertificateOnKonnectEventControlPlaneRef, option.Field)
	assert.NotNil(t, option.ExtractValueFn)

	result := option.ExtractValueFn(&konnectv1alpha1.KonnectEventDataPlaneCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-cp",
				},
			},
		},
	})
	assert.Equal(t, []string{"event-cp"}, result)
}

func TestKonnectEventDataPlaneCertificateOnKonnectEventControlPlaneRef(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for nil object",
			input:    nil,
			expected: nil,
		},
		{
			name:     "returns nil for wrong type",
			input:    &konnectv1alpha2.KonnectExtension{},
			expected: nil,
		},
		{
			name: "returns nil when namespaced ref is empty",
			input: &konnectv1alpha1.KonnectEventDataPlaneCertificate{
				Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{},
			},
			expected: nil,
		},
		{
			name: "returns gateway ref name",
			input: &konnectv1alpha1.KonnectEventDataPlaneCertificate{
				Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
					GatewayRef: commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name: "event-cp",
						},
					},
				},
			},
			expected: []string{"event-cp"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, konnectEventDataPlaneCertificateOnKonnectEventControlPlaneRef(tc.input))
		})
	}
}
