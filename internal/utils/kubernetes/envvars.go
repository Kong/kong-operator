package kubernetes

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// -----------------------------------------------------------------------------
// Kubernetes Utils - EnvVars
// -----------------------------------------------------------------------------

// IsEnvVarPresent indicates whether or not a given EnvVar is present in a list.
func IsEnvVarPresent(envVar corev1.EnvVar, envVars []corev1.EnvVar) bool {
	for _, listVar := range envVars {
		if envVar.Name == listVar.Name {
			return true
		}
	}
	return false
}

// EnvValueByName returns the value of the first env var with the given name.
// If no env var with the given name is found, an empty string is returned.
func EnvValueByName(env []corev1.EnvVar, name string) string {
	for _, envVar := range env {
		if envVar.Name == name {
			return envVar.Value
		}
	}
	return ""
}

// EnvVarSourceByName returns the ValueFrom of the first env var with the given name.
// returns nil if env var is not found, or does not have a ValueFrom field.
func EnvVarSourceByName(env []corev1.EnvVar, name string) *corev1.EnvVarSource {
	for _, envVar := range env {
		if envVar.Name == name {
			return envVar.ValueFrom
		}
	}
	return nil
}

// UpdateEnv set env var with name to have val and returns the updated env vars.
// If no env var with the given `name“ is found, a new env var is appended to the list.
func UpdateEnv(envVars []corev1.EnvVar, name, val string) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	var updated bool
	for _, envVar := range envVars {
		if envVar.Name == name {
			envVar.Value = val
			newEnvVars = append(newEnvVars, envVar)
			updated = true
		} else {
			newEnvVars = append(newEnvVars, envVar)
		}
	}
	if !updated {
		newEnvVars = append(newEnvVars, corev1.EnvVar{
			Name:  name,
			Value: val,
		})
	}

	return newEnvVars
}

// UpdateEnvSource updates env var with `name` to come from `envSource`.
// If no env var with the given `name` is found, a new env var is appended to the list.
func UpdateEnvSource(envVars []corev1.EnvVar, name string, envSource *corev1.EnvVarSource) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	var updated bool
	for _, envVar := range envVars {
		if envVar.Name == name {
			newEnvVars = append(newEnvVars, corev1.EnvVar{
				Name:      name,
				ValueFrom: envSource,
			})
			updated = true
		} else {
			newEnvVars = append(newEnvVars, envVar)
		}
	}
	if !updated {
		newEnvVars = append(newEnvVars, corev1.EnvVar{
			Name:      name,
			ValueFrom: envSource,
		})
	}

	return newEnvVars
}

// RejectEnvByName returns a copy of the given env vars,
// but with the env vars with the given name removed.
func RejectEnvByName(envVars []corev1.EnvVar, name string) []corev1.EnvVar {
	newEnvVars := make([]corev1.EnvVar, 0, len(envVars))
	for _, envVar := range envVars {
		if envVar.Name != name {
			newEnvVars = append(newEnvVars, envVar)
		}
	}
	return newEnvVars
}

// -----------------------------------------------------------------------------
// Kubernetes Utils - Sortable EnvVars
// -----------------------------------------------------------------------------

// SortableEnvVars is a wrapper around []corev1.EnvVars that enables sorting
// them lexographically by name.
type SortableEnvVars []corev1.EnvVar

func (s SortableEnvVars) Len() int { return len(s) }

func (s SortableEnvVars) Less(i, j int) bool {
	iv := s[i].Name + s[i].Value
	jv := s[j].Name + s[j].Value
	return strings.Compare(iv, jv) == -1
}

func (s SortableEnvVars) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
