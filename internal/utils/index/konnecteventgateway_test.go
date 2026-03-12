package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestKonnectEventGatewayAPIAuthConfigurationRef(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KonnectEventGateway object",
			input:    &konnectv1alpha1.KonnectAPIAuthConfiguration{},
			expected: nil,
		},
		{
			name:     "returns auth ref name",
			input:    &konnectv1alpha1.KonnectEventGateway{
				Spec: konnectv1alpha1.KonnectEventGatewaySpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: "my-auth",
						},
					},
				},
			},
			expected: []string{"my-auth"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := konnectEventGatewayAPIAuthConfigurationRef(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOptionsForKonnectEventGateway(t *testing.T) {
	options := OptionsForKonnectEventGateway()
	require.Len(t, options, 1)
	opt := options[0]
	require.IsType(t, &konnectv1alpha1.KonnectEventGateway{}, opt.Object)
	require.Equal(t, IndexFieldKonnectEventGatewayOnAPIAuthConfiguration, opt.Field)
	require.NotNil(t, opt.ExtractValueFn)
}
