package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
			name: "returns correct index if SecretRef is set with namespace",
			input: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Namespace: new("ns"),
						Name:      "mysecret",
					},
				},
			},
			expected: []string{"ns/mysecret"},
		},
		{
			name: "returns correct index if SecretRef is set without namespace",
			input: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "mysecret",
					},
				},
			},
			expected: []string{"default/mysecret"},
		},
		{
			name: "returns correct index for both SecretRef and SecretRefAlt",
			input: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "mysecret",
					},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{
						Namespace: new("other-ns"),
						Name:      "othersecret",
					},
				},
			},
			expected: []string{"default/mysecret", "other-ns/othersecret"},
		},
		{
			name: "returns correct index if only SecretRefAlt is set with namespace",
			input: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRefAlt: &commonv1alpha1.NamespacedRef{
						Namespace: new("ns"),
						Name:      "mysecret",
					},
				},
			},
			expected: []string{"ns/mysecret"},
		},
		{
			name: "returns correct index if only SecretRefAlt is set without namespace",
			input: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRefAlt: &commonv1alpha1.NamespacedRef{
						Name: "mysecret",
					},
				},
			},
			expected: []string{"default/mysecret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SecretOnKongCertificate(tt.input)
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
