package index

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

func TestSecretOnKongCACertificate(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KongCACertificate object",
			input:    &configurationv1alpha1.KongCredentialAPIKey{},
			expected: nil,
		},
		{
			name:     "returns nil if SecretRef is nil",
			input:    &configurationv1alpha1.KongCACertificate{},
			expected: nil,
		},
		{
			name: "returns correct index if SecretRef is set with namespace",
			input: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Namespace: lo.ToPtr("ns"),
						Name:      "mysecret",
					},
				},
			},
			expected: []string{"ns/mysecret"},
		},
		{
			name: "returns correct index if SecretRef is set without namespace",
			input: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "mysecret",
					},
				},
			},
			expected: []string{"default/mysecret"},
		},
		{
			name: "returns correct index if SecretRef namespace is empty string",
			input: &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Namespace: lo.ToPtr(""),
						Name:      "mysecret",
					},
				},
			},
			expected: []string{"default/mysecret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SecretOnKongCACertificate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOptionsForKongCACertificate(t *testing.T) {
	options := OptionsForKongCACertificate(nil)
	require.Len(t, options, 2)
	opt0 := options[0]
	require.IsType(t, &configurationv1alpha1.KongCACertificate{}, opt0.Object)
	require.Equal(t, IndexFieldKongCACertificateOnKonnectGatewayControlPlane, opt0.Field)
	require.NotNil(t, opt0.ExtractValueFn)
	opt1 := options[1]
	require.IsType(t, &configurationv1alpha1.KongCACertificate{}, opt1.Object)
	require.Equal(t, IndexFieldKongCACertificateReferencesSecrets, opt1.Field)
	require.NotNil(t, opt1.ExtractValueFn)
}
