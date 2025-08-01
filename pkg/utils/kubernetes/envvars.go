package kubernetes

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrExtractValueFromEnvSourceNotImplemented is the error when the Env references value from `ResourceRef` or `FieldRef`
// that we do not support to extract values yet.
var ErrExtractValueFromEnvSourceNotImplemented = errors.New("EnvSource type not implemented for extracting value yet")

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

// GetEnvValueFromContainer returns value of environment variable with given name in the given container.
// It returns true in the second return value if the env var is found in any of the following formats:
//   - Directly given in `value` of an item in `envs` of the container.
//   - Fetched from given record of `ConfigMap` or `Secret` in `valueFrom` of an `env` item
//   - Fetched from the record of `ConfigMap` or `Secret` in an `envFrom` item,
//     where name is concatated from `envFrom.Prefix` and key of record in `ConfigMap` or `SecretMap`.
//
// It returns a non-nil error if error happens in fetching the value.
func GetEnvValueFromContainer(ctx context.Context, container *corev1.Container, namespace, name string, c client.Client) (value string, found bool, err error) {
	for _, envVar := range container.Env {
		if envVar.Name == name {
			found = true
			if envVar.Value != "" {
				value = envVar.Value
			}
			if envVar.ValueFrom != nil {
				v, err := fetchEnvValueFromEnvVarSource(ctx, c, namespace, envVar.ValueFrom)
				if err != nil {
					// If the error returned from fetching value from envVarSource is other than "not implemented".
					if !errors.Is(err, ErrExtractValueFromEnvSourceNotImplemented) {
						return "", false, err
					}
				}
				if v != "" {
					value = v
				}
			}
		}
	}
	if found {
		return value, true, nil
	}

	for _, envVarFrom := range container.EnvFrom {
		if strings.HasPrefix(name, envVarFrom.Prefix) {
			v, err := fetchEnvValueFromEnvFromSource(ctx, c, namespace, name, envVarFrom)
			if err != nil {
				return "", false, err
			}
			if v != "" {
				value = v
				found = true
			}
		}
	}
	if found {
		return value, true, nil
	}
	return "", false, nil
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

// fetchEnvValueFromEnvVarSource fetches the value for environment variable from data in ConfigMap or Secret
// where name and key is in the `ValueFrom` of an item in contains's `Env` list.
func fetchEnvValueFromEnvVarSource(ctx context.Context, c client.Client, namespace string, valueSource *corev1.EnvVarSource) (value string, err error) {
	if valueSource.ConfigMapKeyRef != nil {
		cm := &corev1.ConfigMap{}
		keyRef := valueSource.ConfigMapKeyRef
		namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: keyRef.Name}
		err := c.Get(ctx, namespacedName, cm)
		if err != nil {
			return "", err
		}
		if cm.Data != nil && cm.Data[keyRef.Key] != "" {
			return cm.Data[keyRef.Key], nil
		}
	}
	if valueSource.SecretKeyRef != nil {
		secret := &corev1.Secret{}
		keyRef := valueSource.SecretKeyRef
		namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: keyRef.Name}
		err := c.Get(ctx, namespacedName, secret)
		if err != nil {
			return "", err
		}
		if secret.Data != nil && secret.Data[keyRef.Key] != nil {
			decoded, err := base64.StdEncoding.DecodeString(string(secret.Data[keyRef.Key]))
			return string(decoded), err
		}
	}
	// Extracting values from ResourceRef and FieldRef is not supported,
	// so we return a "Not Implemented" error here if ResourceRef or FieldRef is present.
	return "", ErrExtractValueFromEnvSourceNotImplemented
}

// fetchEnvValueFromEnvFromSource fetches value from ConfigMap or Secret specified in container's `EnvFrom` list
// where the concatation of prefix and key of ConfigMap or Secret combines to the spefified env key.
// For example, the `EnvFrom` has the prefix `KONG_` and a data item in the ConfigMap or Secret specified in the Ref has key `VERSION`,
// then we will return the value of the ConfigMap/Secret item if the given key is `KONG_VERSION`.
func fetchEnvValueFromEnvFromSource(ctx context.Context, c client.Client, namespace string, key string, source corev1.EnvFromSource) (value string, err error) {
	suffix := strings.TrimPrefix(key, source.Prefix)
	if source.ConfigMapRef != nil {
		cm := &corev1.ConfigMap{}
		cmRef := source.ConfigMapRef
		namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: cmRef.Name}
		err := c.Get(ctx, namespacedName, cm)
		if err != nil {
			return "", err
		}
		if cm.Data != nil && cm.Data[suffix] != "" {
			return cm.Data[suffix], nil
		}
	}
	if source.SecretRef != nil {
		secret := &corev1.Secret{}
		secretRef := source.SecretRef
		namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: secretRef.Name}
		err := c.Get(ctx, namespacedName, secret)
		if err != nil {
			return "", err
		}
		if secret.Data != nil && secret.Data[suffix] != nil {
			decoded, err := base64.StdEncoding.DecodeString(string(secret.Data[suffix]))
			return string(decoded), err
		}
	}
	return "", nil
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
