package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestOptionsForKonnectAPIAuthConfiguration(t *testing.T) {
	options := OptionsForKonnectAPIAuthConfiguration()

	require.Len(t, options, 1, "should return exactly one index option")

	option := options[0]
	assert.IsType(t, &konnectv1alpha1.KonnectAPIAuthConfiguration{}, option.Object, "should index KonnectAPIAuthConfiguration objects")
	assert.Equal(t, IndexFieldKonnectAPIAuthConfigurationReferencesSecrets, option.Field, "should use correct field name")
	assert.NotNil(t, option.ExtractValueFn, "should have extract value function")

	// Test that the extract function works as expected.
	testAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-auth",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
			SecretRef: &corev1.SecretReference{
				Name:      "test-secret",
				Namespace: "default",
			},
		},
	}

	result := option.ExtractValueFn(testAuth)
	assert.Equal(t, []string{"default/test-secret"}, result, "extract function should work correctly")
}

func TestSecretsOnKonnectAPIAuthConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KonnectAPIAuthConfiguration object",
			input:    &configurationv1alpha1.KongCredentialAPIKey{},
			expected: nil,
		},
		{
			name:     "returns nil for nil object",
			input:    nil,
			expected: nil,
		},
		{
			name: "returns nil if auth type is not SecretRef",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:  konnectv1alpha1.KonnectAPIAuthTypeToken,
					Token: "some-token",
				},
			},
			expected: nil,
		},
		{
			name: "returns nil if SecretRef is nil",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: nil,
				},
			},
			expected: nil,
		},
		{
			name: "returns correct index if SecretRef is set with explicit namespace",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "custom-ns",
					},
				},
			},
			expected: []string{"custom-ns/test-secret"},
		},
		{
			name: "returns correct index if SecretRef is set without namespace (uses object namespace)",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "",
					},
				},
			},
			expected: []string{"default/test-secret"},
		},
		{
			name: "returns correct index for secret in different namespace",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "konnect-system",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "konnect-credentials",
						Namespace: "konnect-system",
					},
				},
			},
			expected: []string{"konnect-system/konnect-credentials"},
		},
		{
			name: "handles empty secret name",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "",
						Namespace: "default",
					},
				},
			},
			expected: []string{"default/"},
		},
		{
			name: "SecretRef type with both Token and SecretRef set (SecretRef should win)",
			input: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:  konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					Token: "should-be-ignored",
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			expected: []string{"default/test-secret"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := secretsOnKonnectAPIAuthConfiguration(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
