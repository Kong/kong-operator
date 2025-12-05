package manager

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateCacheByObject(t *testing.T) {
	tests := []struct {
		name                   string
		cfg                    Config
		expectError            bool
		expectNil              bool
		expectedConfigMapLabel string
		expectedSecretLabel    string
	}{
		{
			name: "no label selectors returns nil",
			cfg: Config{
				ConfigMapLabelSelector: "",
				SecretLabelSelector:    "",
			},
			expectError: false,
			expectNil:   true,
		},
		{
			name: "only secret label selector",
			cfg: Config{
				SecretLabelSelector: "app",
			},
			expectError:         false,
			expectNil:           false,
			expectedSecretLabel: "app",
		},
		{
			name: "only configmap label selector",
			cfg: Config{
				ConfigMapLabelSelector: "configmap.konghq.com",
			},
			expectError:            false,
			expectNil:              false,
			expectedConfigMapLabel: "configmap.konghq.com",
		},
		{
			name: "both label selectors",
			cfg: Config{
				ConfigMapLabelSelector: "configmap.konghq.com",
				SecretLabelSelector:    "app",
			},
			expectError:            false,
			expectNil:              false,
			expectedConfigMapLabel: "configmap.konghq.com",
			expectedSecretLabel:    "app",
		},
		{
			name: "invalid secret label selector",
			cfg: Config{
				SecretLabelSelector: "invalid==label",
			},
			expectError: true,
		},
		{
			name: "invalid configmap label selector",
			cfg: Config{
				ConfigMapLabelSelector: "invalid==label",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := createCacheByObject(tt.cfg)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.expectNil {
				require.Nil(t, result)
				return
			}

			require.NotNil(t, result)

			if tt.expectedSecretLabel != "" {
				for obj, v := range result {
					if _, ok := obj.(*corev1.Secret); ok {
						r, _ := v.Label.Requirements()
						require.Len(t, r, 1)
						require.Equal(t, tt.expectedSecretLabel, r[0].Key())
					}
				}
			}

			if tt.expectedConfigMapLabel != "" {
				for obj, v := range result {
					if _, ok := obj.(*corev1.ConfigMap); ok {
						r, _ := v.Label.Requirements()
						require.Len(t, r, 1)
						require.Equal(t, tt.expectedConfigMapLabel, r[0].Key())
					}
				}
			}
		})
	}
}
