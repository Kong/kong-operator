package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestSetContainerEnv(t *testing.T) {
	testCases := []struct {
		name         string
		container    corev1.Container
		env          corev1.EnvVar
		expectedEnvs []corev1.EnvVar
	}{
		{
			name: "env with same name does not exist",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "ENV_0",
						Value: "value_0",
					},
				},
			},
			env: corev1.EnvVar{
				Name:  "ENV_1",
				Value: "value_1",
			},
			expectedEnvs: []corev1.EnvVar{
				{
					Name:  "ENV_0",
					Value: "value_0",
				},
				{
					Name:  "ENV_1",
					Value: "value_1",
				},
			},
		},
		{
			name: "one item in env with same name",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "ENV_1",
						Value: "value_0",
					},
				},
			},
			env: corev1.EnvVar{
				Name:  "ENV_1",
				Value: "value_1",
			},
			expectedEnvs: []corev1.EnvVar{
				{
					Name:  "ENV_1",
					Value: "value_1",
				},
			},
		},
		{
			name: "multiple items in env with same name",
			container: corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "ENV_1",
						Value: "value_0",
					},
					{
						Name:  "ENV_1",
						Value: "value_2",
					},
				},
			},
			env: corev1.EnvVar{
				Name:  "ENV_1",
				Value: "value_1",
			},
			expectedEnvs: []corev1.EnvVar{
				{
					Name:  "ENV_1",
					Value: "value_1",
				},
				{
					Name:  "ENV_1",
					Value: "value_1",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetContainerEnv(&tc.container, tc.env)
			require.Equal(t, tc.expectedEnvs, tc.container.Env)
		})

	}
}
