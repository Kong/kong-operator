package controlplane

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

func TestValidator_ValidateDeploymentOptions(t *testing.T) {
	tests := []struct {
		name    string
		v       *Validator
		opts    *operatorv1alpha1.DeploymentOptions
		wantErr bool
	}{
		{
			name: "specifying just the image works",
			v:    &Validator{},
			opts: &operatorv1alpha1.DeploymentOptions{
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
			name: "not specifying the image is an error",
			v:    &Validator{},
			opts: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "controller",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "not specifying the controller container is an error",
			v:       &Validator{},
			opts:    &operatorv1alpha1.DeploymentOptions{},
			wantErr: true,
		},
		{
			// TODO: https://github.com/Kong/gateway-operator/issues/736
			name: "using more than 1 replica is an error",
			v:    &Validator{},
			opts: &operatorv1alpha1.DeploymentOptions{
				Replicas: lo.ToPtr(int32(2)),
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "controller",
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "volumes and volume mounts can be specified on ControlPlane deployment options",
			v:    &Validator{},
			opts: &operatorv1alpha1.DeploymentOptions{
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
