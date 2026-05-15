package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestKongUpstreamRefersToKongCertificate(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KongUpstream object",
			input:    &configurationv1alpha1.KongCertificate{},
			expected: nil,
		},
		{
			name:     "returns nil when ClientCertificateRef is nil",
			input:    &configurationv1alpha1.KongUpstream{},
			expected: nil,
		},
		{
			name: "same-NS ref (nil namespace) uses object namespace",
			input: &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{
							Name: "cert",
						},
					},
				},
			},
			expected: []string{"default/cert"},
		},
		{
			name: "explicit cross-NS ref uses specified namespace",
			input: &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{
							Name:      "cert",
							Namespace: new("other-ns"),
						},
					},
				},
			},
			expected: []string{"other-ns/cert"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kongUpstreamRefersToKongCertificate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
