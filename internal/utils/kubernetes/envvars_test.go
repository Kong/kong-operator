package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestUpdateEnv(t *testing.T) {

	exampleVarSource := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			FieldPath: "metadata.name",
		},
	}

	for _, tc := range []struct {
		name     string
		varName  string
		varValue string
		envVars  []corev1.EnvVar
		expected []corev1.EnvVar
	}{
		{
			name:     "update value in env vars",
			varName:  "ENV_VAR_2",
			varValue: "new_value",
			envVars: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", Value: "value2"},
				{Name: "ENV_VAR_3", ValueFrom: exampleVarSource},
			},
			expected: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", Value: "new_value"},
				{Name: "ENV_VAR_3", ValueFrom: exampleVarSource},
			},
		},
		{
			name:     "non-existent env var is appended",
			varName:  "ENV_VAR_4",
			varValue: "value4",
			envVars: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", ValueFrom: exampleVarSource},
				{Name: "ENV_VAR_3", Value: "value3"},
			},
			expected: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", ValueFrom: exampleVarSource},
				{Name: "ENV_VAR_3", Value: "value3"},
				{Name: "ENV_VAR_4", Value: "value4"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, UpdateEnv(tc.envVars, tc.varName, tc.varValue))
		})
	}
}

func TestUpdateEnvSource(t *testing.T) {

	exampleVarSource := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			FieldPath: "metadata.name",
		},
	}

	for _, tc := range []struct {
		name      string
		varName   string
		varSource *corev1.EnvVarSource
		envVars   []corev1.EnvVar
		expected  []corev1.EnvVar
	}{
		{
			name:      "update value in env vars",
			varName:   "ENV_VAR_2",
			varSource: exampleVarSource,
			envVars: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", Value: "value2"},
				{Name: "ENV_VAR_3", Value: "value3"},
			},
			expected: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", ValueFrom: exampleVarSource},
				{Name: "ENV_VAR_3", Value: "value3"},
			},
		},
		{
			name:      "non-existent env var",
			varName:   "ENV_VAR_4",
			varSource: exampleVarSource,
			envVars: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", ValueFrom: exampleVarSource},
				{Name: "ENV_VAR_3", Value: "value3"},
			},
			expected: []corev1.EnvVar{
				{Name: "ENV_VAR_1", Value: "value1"},
				{Name: "ENV_VAR_2", ValueFrom: exampleVarSource},
				{Name: "ENV_VAR_3", Value: "value3"},
				{Name: "ENV_VAR_4", ValueFrom: exampleVarSource},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, UpdateEnvSource(tc.envVars, tc.varName, tc.varSource))
		})
	}
}
