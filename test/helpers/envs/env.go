package envs

import corev1 "k8s.io/api/core/v1"

// SetValueByName sets the EnvVar in slice with the provided name and value.
func SetValueByName(envs []corev1.EnvVar, name string, value string) []corev1.EnvVar {
	for i := range envs {
		env := &envs[i]
		if env.Name == name {
			env.Value = value
			return envs
		}
	}
	return append(envs, corev1.EnvVar{
		Name:  name,
		Value: value,
	})
}

// GetValueByName returns the corresponding value of the LAST item with the given name, for non-existing name returns empty string.
func GetValueByName(envs []corev1.EnvVar, name string) string {
	value := ""
	for _, env := range envs {
		if env.Name == name {
			value = env.Value
		}
	}
	return value
}
