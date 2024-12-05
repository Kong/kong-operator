package envs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestSetValueByName(t *testing.T) {
	tests := []struct {
		name     string
		envs     []corev1.EnvVar
		setName  string
		setValue string
		expected []corev1.EnvVar
	}{
		{
			name: "set new env var",
			envs: []corev1.EnvVar{
				{Name: "EXISTING_VAR", Value: "existing_value"},
			},
			setName:  "NEW_VAR",
			setValue: "new_value",
			expected: []corev1.EnvVar{
				{Name: "EXISTING_VAR", Value: "existing_value"},
				{Name: "NEW_VAR", Value: "new_value"},
			},
		},
		{
			name: "update existing env var",
			envs: []corev1.EnvVar{
				{Name: "EXISTING_VAR", Value: "existing_value"},
			},
			setName:  "EXISTING_VAR",
			setValue: "updated_value",
			expected: []corev1.EnvVar{
				{Name: "EXISTING_VAR", Value: "updated_value"},
			},
		},
		{
			name:     "empty envs slice",
			envs:     []corev1.EnvVar{},
			setName:  "NEW_VAR",
			setValue: "new_value",
			expected: []corev1.EnvVar{
				{Name: "NEW_VAR", Value: "new_value"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, SetValueByName(tt.envs, tt.setName, tt.setValue))
		})
	}
}
