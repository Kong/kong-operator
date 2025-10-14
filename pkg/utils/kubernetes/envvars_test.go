package kubernetes

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestGetEnvValueFromContainer(t *testing.T) {
	defaultObjects := []client.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm"},
			Data: map[string]string{
				"off": "off",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret"},
			// fake client does not encode fields in StringData to Data,
			// so here we should use base64 encoded value in Data.
			Data: map[string][]byte{
				"postgres": []byte(base64.StdEncoding.EncodeToString([]byte("postgres"))),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm-2"},
			Data: map[string]string{
				"KONG_DATABASE": "xxx",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret-2"},
			// fake client does not encode fields in StringData to Data,
			// so here we should use base64 encoded value in Data.
			Data: map[string][]byte{
				"DATABASE": []byte(base64.StdEncoding.EncodeToString([]byte("xxx"))),
			},
		},
	}

	testCases := []struct {
		name          string
		container     *corev1.Container
		key           string
		expectedValue string
		found         bool
		hasError      bool
	}{
		{
			name: "get value from Value of env",
			container: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name:  "KONG_DATABASE",
						Value: "off",
					},
				},
			},
			key:           "KONG_DATABASE",
			expectedValue: "off",
			found:         true,
			hasError:      false,
		},
		{
			name: "get value from configMap key in ValueFrom of env",
			container: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name: "KONG_DATABASE",
						ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm"},
								Key:                  "off",
							},
						},
					},
				},
			},
			key:           "KONG_DATABASE",
			expectedValue: "off",
			found:         true,
			hasError:      false,
		},
		{
			name: "get Value from Secret key in ValueFrom of env",
			container: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name: "KONG_DATABASE",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "postgres",
							},
						},
					},
				},
			},
			key:           "KONG_DATABASE",
			expectedValue: "postgres",
			found:         true,
			hasError:      false,
		},
		{
			name: "cannot find given key in env",
			container: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name: "KONG_DATABASE",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
								Key:                  "secret",
							},
						},
					},
				},
			},
			key:           "KONG_VERSION",
			expectedValue: "",
			found:         false,
			hasError:      false,
		},
		{
			name: "error in fetching value from configMap",
			container: &corev1.Container{
				Env: []corev1.EnvVar{
					{
						Name: "KONG_DATABASE",
						ValueFrom: &corev1.EnvVarSource{
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm-notexist"},
								Key:                  "off",
							},
						},
					},
				},
			},
			key:      "KONG_DATABASE",
			hasError: true,
		},
		{
			name: "find value from referenced configMap in EnvFrom",
			container: &corev1.Container{
				EnvFrom: []corev1.EnvFromSource{
					{
						Prefix: "",
						ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm-2"},
						},
					},
				},
			},
			key:           "KONG_DATABASE",
			expectedValue: "xxx",
			found:         true,
			hasError:      false,
		},
		{
			name: "find value from referenced Secret in EnvFrom with prefix",
			container: &corev1.Container{
				EnvFrom: []corev1.EnvFromSource{
					{
						Prefix: "KONG_",
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret-2"},
						},
					},
				},
			},
			key:           "KONG_DATABASE",
			expectedValue: "xxx",
			found:         true,
			hasError:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeclient.NewClientBuilder().
				WithObjects(defaultObjects...).
				Build()

			value, found, err := GetEnvValueFromContainer(t.Context(), tc.container, "default", tc.key, c)
			if tc.hasError {
				require.Error(t, err)
				return
			}
			if !tc.found {
				require.False(t, found)
				return
			}
			require.True(t, found)
			require.Equal(t, tc.expectedValue, value)
		})
	}
}
