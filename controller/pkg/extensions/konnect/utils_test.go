package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestKonnectExtensionToExtensionRef(t *testing.T) {
	tests := []struct {
		name      string
		ns        *string
		extension *konnectv1alpha2.KonnectExtension
		want      *commonv1alpha1.ExtensionRef
	}{
		{
			name:      "nil extension returns nil",
			ns:        new("default"),
			extension: nil,
			want:      nil,
		},
		{
			name:      "nil extension with nil ns returns nil",
			ns:        nil,
			extension: nil,
			want:      nil,
		},
		{
			name: "ns is used as namespace in the result",
			ns:   new("default"),
			extension: &konnectv1alpha2.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-extension",
					Namespace: "default",
				},
			},
			want: &commonv1alpha1.ExtensionRef{
				Group: konnectv1alpha2.SchemeGroupVersion.Group,
				Kind:  konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{
					Name:      "test-extension",
					Namespace: new("default"),
				},
			},
		},
		{
			name: "ns differs from extension namespace",
			ns:   new("other-namespace"),
			extension: &konnectv1alpha2.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-extension",
					Namespace: "extension-namespace",
				},
			},
			want: &commonv1alpha1.ExtensionRef{
				Group: konnectv1alpha2.SchemeGroupVersion.Group,
				Kind:  konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{
					Name:      "test-extension",
					Namespace: new("other-namespace"),
				},
			},
		},
		{
			name: "nil ns results in nil namespace",
			ns:   nil,
			extension: &konnectv1alpha2.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-extension",
					Namespace: "default",
				},
			},
			want: &commonv1alpha1.ExtensionRef{
				Group: konnectv1alpha2.SchemeGroupVersion.Group,
				Kind:  konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{
					Name:      "test-extension",
					Namespace: nil,
				},
			},
		},
		{
			name: "ns is used regardless of extension metadata",
			ns:   new("production"),
			extension: &konnectv1alpha2.KonnectExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-extension",
					Namespace: "staging",
					Labels: map[string]string{
						"app": "test",
					},
					Annotations: map[string]string{
						"description": "test extension",
					},
				},
				Spec: konnectv1alpha2.KonnectExtensionSpec{
					// Fields here should not affect the conversion
				},
			},
			want: &commonv1alpha1.ExtensionRef{
				Group: konnectv1alpha2.SchemeGroupVersion.Group,
				Kind:  konnectv1alpha2.KonnectExtensionKind,
				NamespacedRef: commonv1alpha1.NamespacedRef{
					Name:      "complex-extension",
					Namespace: new("production"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KonnectExtensionToExtensionRef(tt.ns, tt.extension)
			assert.Equal(t, tt.want, got)
		})
	}
}
