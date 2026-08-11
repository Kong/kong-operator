package manager

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestCreateCacheByObject(t *testing.T) {
	tests := []struct {
		name                   string
		cfg                    Config
		expectError            bool
		expectedConfigMapLabel string
		expectedSecretLabel    string
	}{
		{
			name: "no label selectors still registers the CRD schema-stripping entry",
			cfg: Config{
				ConfigMapLabelSelector: "",
				SecretLabelSelector:    "",
			},
			expectError: false,
		},
		{
			name: "only secret label selector",
			cfg: Config{
				SecretLabelSelector: "app",
			},
			expectError:         false,
			expectedSecretLabel: "app",
		},
		{
			name: "only configmap label selector",
			cfg: Config{
				ConfigMapLabelSelector: "configmap.konghq.com",
			},
			expectError:            false,
			expectedConfigMapLabel: "configmap.konghq.com",
		},
		{
			name: "both label selectors",
			cfg: Config{
				ConfigMapLabelSelector: "configmap.konghq.com",
				SecretLabelSelector:    "app",
			},
			expectError:            false,
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
			require.NotNil(t, result)

			// The CRD schema-stripping entry is registered unconditionally, regardless of
			// Secret/ConfigMap label selector configuration.
			var foundCRDEntry bool
			for obj := range result {
				if _, ok := obj.(*apiextensionsv1.CustomResourceDefinition); ok {
					foundCRDEntry = true
				}
			}
			require.True(t, foundCRDEntry, "expected a cache.ByObject entry for CustomResourceDefinition")

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

func TestCRDCacheByObjectTransform(t *testing.T) {
	newCRD := func(group string) *apiextensionsv1.CustomResourceDefinition {
		return &apiextensionsv1.CustomResourceDefinition{
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: group,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1",
						Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{}},
					},
				},
			},
		}
	}

	transform := crdCacheByObject().Transform
	require.NotNil(t, transform)

	t.Run("strips schema for CRDs outside ssaCRDGroups", func(t *testing.T) {
		out, err := transform(newCRD("gateway-operator.konghq.com"))
		require.NoError(t, err)
		crd, ok := out.(*apiextensionsv1.CustomResourceDefinition)
		require.True(t, ok)
		require.Nil(t, crd.Spec.Versions[0].Schema)
	})

	t.Run("keeps schema for CRDs in ssaCRDGroups", func(t *testing.T) {
		for group := range ssaCRDGroups {
			out, err := transform(newCRD(group))
			require.NoError(t, err)
			crd, ok := out.(*apiextensionsv1.CustomResourceDefinition)
			require.True(t, ok)
			require.NotNil(t, crd.Spec.Versions[0].Schema, "group %q should keep its schema", group)
		}
	})

	t.Run("passes through non-CRD objects unchanged", func(t *testing.T) {
		secret := &corev1.Secret{}
		out, err := transform(secret)
		require.NoError(t, err)
		require.Same(t, secret, out)
	})
}
