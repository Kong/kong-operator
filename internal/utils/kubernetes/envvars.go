package kubernetes

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// -----------------------------------------------------------------------------
// Kubernetes Utils - EnvVars
// -----------------------------------------------------------------------------

// IsEnvVarPresent indicates whether or not a given EnvVar is present in a list
func IsEnvVarPresent(envVar corev1.EnvVar, envVars []corev1.EnvVar) (found bool) {
	for _, listVar := range envVars {
		if envVar.Name == listVar.Name {
			found = true
		}
	}
	return
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
