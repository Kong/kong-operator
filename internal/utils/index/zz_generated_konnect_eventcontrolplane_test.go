package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestOptionsForKonnectEventControlPlane(t *testing.T) {
	options := OptionsForKonnectEventControlPlane()

	require.Len(t, options, 1)

	option := options[0]
	assert.IsType(t, &konnectv1alpha1.KonnectEventControlPlane{}, option.Object)
	assert.Equal(t, IndexFieldKonnectEventControlPlaneOnAPIAuthConfiguration, option.Field)
	assert.NotNil(t, option.ExtractValueFn)

	result := option.ExtractValueFn(&konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-cp",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectEventControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "auth",
				},
			},
		},
	})
	assert.Equal(t, []string{"default/auth"}, result)
}

func TestKonnectEventControlPlaneAPIAuthConfigurationRef(t *testing.T) {
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
			name: "returns nil when auth ref name is empty",
			input: &konnectv1alpha1.KonnectEventControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectEventControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{},
				},
			},
			expected: nil,
		},
		{
			name: "returns namespace scoped auth ref key",
			input: &konnectv1alpha1.KonnectEventControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectEventControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: "auth",
						},
					},
				},
			},
			expected: []string{"default/auth"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, konnectEventControlPlaneAPIAuthConfigurationRef(tc.input))
		})
	}
}
