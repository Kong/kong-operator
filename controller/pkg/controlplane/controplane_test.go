package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestDeduceAnonymousReportsEnabled(t *testing.T) {
	tests := []struct {
		name                    string
		anonymousReportsEnabled bool
		cpOpts                  *operatorv1beta1.ControlPlaneOptions
		expected                bool
	}{
		{
			name:                    "CP opts anonymous reports not set, anonymous reports disabled",
			anonymousReportsEnabled: false,
			cpOpts:                  &operatorv1beta1.ControlPlaneOptions{},
			expected:                false,
		},
		{
			name:                    "CP opts anonymous reports not set, anonymous reports disabled",
			anonymousReportsEnabled: false,
			expected:                false,
			cpOpts:                  &operatorv1beta1.ControlPlaneOptions{},
		},
		{
			name:                    "CP opts anonymous reports disabled",
			anonymousReportsEnabled: true,
			cpOpts: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.ControlPlaneControllerContainerName,
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "false",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name:                    "CP opts anonymous reports enabled, anonymous reports enabled",
			anonymousReportsEnabled: true,
			cpOpts: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.ControlPlaneControllerContainerName,
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "false",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name:                    "CP opts anonymous reports enabled, anonymous reports disabled",
			anonymousReportsEnabled: false,
			cpOpts: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.ControlPlaneControllerContainerName,
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_ANONYMOUS_REPORTS",
											Value: "true",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := DeduceAnonymousReportsEnabled(tt.anonymousReportsEnabled, tt.cpOpts)
			require.Equal(t, tt.expected, ret)
		})
	}
}
