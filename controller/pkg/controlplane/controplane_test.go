package controlplane

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
)

func TestDeduceAnonymousReportsEnabled(t *testing.T) {
	tests := []struct {
		name            string
		developmentMode bool
		cpOpts          *operatorv1beta1.ControlPlaneOptions
		expected        bool
	}{
		{
			name:            "Anonymous reports not set, development mode enabled",
			developmentMode: true,
			cpOpts:          &operatorv1beta1.ControlPlaneOptions{},
			expected:        false,
		},
		{
			name:            "Anonymous reports not set, development mode disabled",
			developmentMode: true,
			expected:        false,
			cpOpts:          &operatorv1beta1.ControlPlaneOptions{},
		},
		{
			name:            "Anonymous reports disabled",
			developmentMode: false,
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
			name:            "Anonymous reports enabled, development mode disabled",
			developmentMode: false,
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
			name:            "Anonymous reports enabled, development mode",
			developmentMode: true,
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
			ret := DeduceAnonymousReportsEnabled(tt.developmentMode, tt.cpOpts)
			require.Equal(t, tt.expected, ret)
		})
	}
}
