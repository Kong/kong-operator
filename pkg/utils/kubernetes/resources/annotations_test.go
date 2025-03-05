package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/gateway-operator/pkg/consts"
)

func TestSpecHash(t *testing.T) {
	tests := []struct {
		name            string
		podTemplateSpec *corev1.PodTemplateSpec
		want            string
		wantErr         bool
	}{
		{
			name:            "empty spec",
			podTemplateSpec: &corev1.PodTemplateSpec{},
			want:            "d13fb40597a132f0",
			wantErr:         false,
		},
		{
			name: "with podTemplateSpec",
			podTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "proxy",
							Image: "kong:3.9",
						},
					},
				},
			},
			want:    "242951015ff547e8",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := appsv1.Deployment{}
			err1 := AnnotatePodTemplateSpecHash(&deployment, tt.podTemplateSpec)
			if tt.wantErr {
				assert.Error(t, err1)
				return
			}
			require.NoError(t, err1)
			assert.Equal(t, tt.want, deployment.Annotations[consts.AnnotationPodTemplateSpecHash])
		})
	}
}
