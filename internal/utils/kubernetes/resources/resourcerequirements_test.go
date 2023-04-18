package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourceRequirementsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    corev1.ResourceRequirements
		b    *corev1.ResourceRequirements
		want bool
	}{
		{
			name: "empty requirements are equal to nil",
			a:    corev1.ResourceRequirements{},
			b:    nil,
			want: true,
		},
		{
			name: "nil requirements are not equal requirements with CPU limit",
			a:    corev1.ResourceRequirements{},
			b: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse(DefaultControlPlaneCPURequest),
				},
			},
			want: false,
		},
		{
			name: "requirements with CPU request is not equal to nil",
			a: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse(DefaultControlPlaneCPURequest),
				},
			},
			b:    nil,
			want: false,
		},
		{
			name: "equal requirements are equal",
			a: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPURequest),
					corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryRequest),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPULimit),
					corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryLimit),
				},
			},
			b: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPURequest),
					corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryRequest),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(DefaultControlPlaneCPULimit),
					corev1.ResourceMemory: resource.MustParse(DefaultControlPlaneMemoryLimit),
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ResourceRequirementsEqual(tt.a, tt.b))
		})
	}
}
