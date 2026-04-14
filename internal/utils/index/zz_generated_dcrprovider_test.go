package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
)

func TestOptionsForDcrProvider(t *testing.T) {
	options := OptionsForDcrProvider()

	require.Len(t, options, 1)

	option := options[0]
	assert.IsType(t, &xkonnectv1alpha1.DcrProvider{}, option.Object)
	assert.Equal(t, IndexFieldDcrProviderOnAPIAuthConfiguration, option.Field)
	assert.NotNil(t, option.ExtractValueFn)

	result := option.ExtractValueFn(&xkonnectv1alpha1.DcrProvider{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "provider",
			Namespace: "default",
		},
		Spec: xkonnectv1alpha1.DcrProviderSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "auth",
				},
			},
		},
	})
	assert.Equal(t, []string{"default/auth"}, result)
}

func TestDcrProviderAPIAuthConfigurationRef(t *testing.T) {
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
			input: &xkonnectv1alpha1.DcrProvider{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: xkonnectv1alpha1.DcrProviderSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{},
				},
			},
			expected: nil,
		},
		{
			name: "returns namespace scoped auth ref key",
			input: &xkonnectv1alpha1.DcrProvider{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
				Spec: xkonnectv1alpha1.DcrProviderSpec{
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
			assert.Equal(t, tc.expected, dcrProviderAPIAuthConfigurationRef(tc.input))
		})
	}
}
