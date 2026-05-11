package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestKongServiceRefersToKongCertificate(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KongService object",
			input:    &configurationv1alpha1.KongCertificate{},
			expected: nil,
		},
		{
			name:     "returns nil when ClientCertificateRef is nil",
			input:    &configurationv1alpha1.KongService{},
			expected: nil,
		},
		{
			name: "same-NS ref (nil namespace) uses object namespace",
			input: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
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
			input: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
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
			result := kongServiceRefersToKongCertificate(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKongServiceRefersToKongCACertificates(t *testing.T) {
	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "returns nil for non-KongService object",
			input:    &configurationv1alpha1.KongCertificate{},
			expected: nil,
		},
		{
			name: "empty CACertificateRefs returns empty slice",
			input: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			},
			expected: []string{},
		},
		{
			name: "single same-NS ref uses object namespace",
			input: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						CACertificateRefs: []commonv1alpha1.NamespacedRef{
							{Name: "ca1"},
						},
					},
				},
			},
			expected: []string{"default/ca1"},
		},
		{
			name: "multiple refs including cross-NS",
			input: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						CACertificateRefs: []commonv1alpha1.NamespacedRef{
							{Name: "ca1"},
							{Name: "ca2", Namespace: new("other-ns")},
						},
					},
				},
			},
			expected: []string{"default/ca1", "other-ns/ca2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := kongServiceRefersToKongCACertificates(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
