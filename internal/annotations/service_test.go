package annotations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestISServiceUpstream(t *testing.T) {
	tests := []struct {
		name     string
		svc      *corev1.Service
		expected bool
	}{
		{
			name:     "nil",
			svc:      nil,
			expected: false,
		},
		{
			name: "empty annotations",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			name: "service-upstream true",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ingress.kubernetes.io/service-upstream": "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "service-upstream false",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ingress.kubernetes.io/service-upstream": "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "service-upstream invalid value",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"ingress.kubernetes.io/service-upstream": "42",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsServiceUpstream(tt.svc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
