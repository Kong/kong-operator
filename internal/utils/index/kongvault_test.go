package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestKongVaultOnKonnectConfigStoreRef(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for a non-KongVault object",
			input:    &configurationv1alpha1.KongCACertificate{},
			expected: nil,
		},
		{
			name:     "returns nil if configStoreRef is unset",
			input:    &configurationv1alpha1.KongVault{},
			expected: nil,
		},
		{
			name: "indexes the referenced KonnectConfigStore",
			input: &configurationv1alpha1.KongVault{
				// KongVault is cluster-scoped, so the reference namespace is used as-is.
				ObjectMeta: metav1.ObjectMeta{Name: "certvault"},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "konnect",
					Prefix:  "certvault",
					ConfigStoreRef: &configurationv1alpha1.KonnectConfigStoreRef{
						Name:      "tls-cert-keys",
						Namespace: "kong",
					},
				},
			},
			expected: []string{"kong/tls-cert-keys"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, kongVaultOnKonnectConfigStoreRef(tt.input))
		})
	}
}
