package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

func TestValidator_ValidateDeploymentOptions(t *testing.T) {
	tests := []struct {
		name    string
		v       *Validator
		opts    *operatorv1beta1.ControlPlaneDeploymentOptions
		wantErr bool
	}{
		{
			name: "specifying just the image works",
			v:    &Validator{},
			opts: &operatorv1beta1.ControlPlaneDeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "controller",
								Image: "kong/kubernetes-ingress-controller:2.12",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "volumes and volume mounts can be specified on ControlPlane deployment options",
			v:    &Validator{},
			opts: &operatorv1beta1.ControlPlaneDeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "test-volume",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "controller",
								Image: "kong/kubernetes-ingress-controller:2.12",
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "test-volume",
										MountPath: "/test-path",
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			err := v.ValidateDeploymentOptions(tt.opts)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
