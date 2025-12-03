package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

func TestSecretOnKongCertificate(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KongCertificate object",
			input:    &configurationv1alpha1.KongCredentialAPIKey{},
			expected: nil,
		},
		{
			name:     "returns nil if SecretRef is nil",
			input:    &configurationv1alpha1.KongCertificate{},
			expected: nil,
		},
		{
			name: "returns correct index if SecretRef is set",
			input: &configurationv1alpha1.KongCertificate{
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &corev1.SecretReference{
						Namespace: "ns",
						Name:      "mysecret",
					},
				},
			},
			expected: []string{"ns/mysecret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := secretOnKongCertificate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOptionsForKongCertificate(t *testing.T) {
	options := OptionsForKongCertificate(nil)
	require.Len(t, options, 2)
	opt0 := options[0]
	require.IsType(t, &configurationv1alpha1.KongCertificate{}, opt0.Object)
	require.Equal(t, IndexFieldKongCertificateOnKonnectGatewayControlPlane, opt0.Field)
	require.NotNil(t, opt0.ExtractValueFn)
	opt1 := options[1]
	require.IsType(t, &configurationv1alpha1.KongCertificate{}, opt1.Object)
	require.Equal(t, IndexFieldKongCertificateReferencesSecrets, opt1.Field)
	require.NotNil(t, opt1.ExtractValueFn)
}
