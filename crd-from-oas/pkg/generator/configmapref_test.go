package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// TestBuildSensitiveLeaves_ConfigMapStringLeaf_UsesConfigMapDataSource covers
// a string leaf configured with a "ConfigMap" reference type: it must use the
// shared ConfigMapDataSource type (not SensitiveDataSource) both for a direct
// apiSpec field and for a nested schema field.
func TestBuildSensitiveLeaves_ConfigMapStringLeaf_UsesConfigMapDataSource(t *testing.T) {
	sourceSchema := &parser.Schema{
		Name: "FakeSource",
		Properties: []*parser.Property{
			{Name: "handler", Type: "string"},
		},
	}
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "schema", Type: "string"},
			{Name: "source", RefName: "FakeSource"},
			{Name: "apiKey", Type: "string"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakePlugin": entitySchema},
		Schemas:       map[string]*parser.Schema{"FakeSource": sourceSchema},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		DataSources: map[string][]config.DataSourceConfig{
			"FakePlugin": {
				{Path: "spec.apiSpec.schema", Type: "ConfigMap"},
				{Path: "spec.apiSpec.source.handler", Type: "ConfigMap"},
				{Path: "spec.apiSpec.apiKey", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateDataSources("FakePlugin")
	require.Len(t, tmpls, 3)
	assert.True(t, tmpls[0].IsConfigMap)
	assert.Equal(t, "Schema", tmpls[0].GoFieldSelector)
	assert.True(t, tmpls[1].IsConfigMap)
	assert.Equal(t, "Source.Handler", tmpls[1].GoFieldSelector)
	assert.False(t, tmpls[2].IsConfigMap)
	for _, tmpl := range tmpls {
		assert.Equal(t, "string", tmpl.ValueGoType)
		assert.Empty(t, tmpl.DedicatedTypeName)
	}

	assert.True(t, g.hasSecretRefs("FakePlugin"))
	assert.True(t, g.hasConfigMapRefs("FakePlugin"))

	lt, ok := g.schemaFieldSensitiveType("FakeSource", "handler")
	require.True(t, ok)
	assert.Equal(t, configMapDataSourceTypeName, lt.sensitiveGoTypeName())

	content, err := g.generateCRDType("FakePlugin", entitySchema)
	require.NoError(t, err)
	assert.Contains(t, content, "Schema ConfigMapDataSource `json:\"schema,omitzero\"`")
	assert.Contains(t, content, "APIKey SensitiveDataSource")
}

// TestBuildSensitiveLeaves_ConfigMapNonStringLeaf_Errors checks that a
// "ConfigMap" reference is rejected on leaves the shared ConfigMapDataSource
// (a single string value) cannot represent.
func TestBuildSensitiveLeaves_ConfigMapNonStringLeaf_Errors(t *testing.T) {
	tests := []struct {
		name    string
		prop    *parser.Property
		wantErr string
	}{
		{
			name:    "non-string leaf",
			prop:    &parser.Property{Name: "config", Type: "object", AdditionalProperties: &parser.Property{Type: "string"}},
			wantErr: `only string leaves are supported for type "ConfigMap"`,
		},
		{
			name:    "array of strings leaf",
			prop:    &parser.Property{Name: "config", Type: "array", Items: &parser.Property{Type: "string"}},
			wantErr: `array leaves are not supported for type "ConfigMap"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wrapperSchema := &parser.Schema{
				Name:       "FakeWrapper",
				Properties: []*parser.Property{tc.prop},
			}
			entitySchema := &parser.Schema{
				Properties: []*parser.Property{
					{Name: "wrapper", RefName: "FakeWrapper"},
				},
			}
			parsed := &parser.ParsedSpec{
				RequestBodies: map[string]*parser.Schema{"FakePlugin": entitySchema},
				Schemas:       map[string]*parser.Schema{"FakeWrapper": wrapperSchema},
			}
			g := NewGenerator(Config{
				APIVersion: "v1alpha1",
				DataSources: map[string][]config.DataSourceConfig{
					"FakePlugin": {
						{Path: "spec.apiSpec.wrapper.config", Type: "ConfigMap"},
					},
				},
			})
			require.ErrorContains(t, g.buildSensitiveLeaves(parsed), tc.wantErr)
		})
	}
}

func TestGenerateSDKOps_ConfigMapReferenceResolution(t *testing.T) {
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "schema", Type: "string"},
			{Name: "apiKey", Type: "string"},
			{Name: "name", Type: "string"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateFakePluginRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateFakePluginRequest"},
		},
	}

	t.Run("ConfigMap references only", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			DataSources: map[string][]config.DataSourceConfig{
				"FakePlugin": {
					{Path: "spec.apiSpec.schema", Type: "ConfigMap"},
				},
			},
		})
		content, err := g.generateSDKOps("FakePlugin", schema, opsConfig)
		require.NoError(t, err)

		assert.Contains(t, content, `corev1 "k8s.io/api/core/v1"`)
		assert.Contains(t, content, "apiSpec := *obj.Spec.APISpec.DeepCopy()")
		assert.Contains(t, content, "if src.Type == ConfigMapDataSourceTypeConfigMapRef {")
		assert.Contains(t, content, "var configMap corev1.ConfigMap")
		assert.Contains(t, content, "client.ObjectKey{Namespace: obj.GetNamespace(), Name: src.ConfigMapRef.Name}")
		assert.Contains(t, content, "resolved, ok := configMap.Data[src.ConfigMapRef.Key]")
		assert.Contains(t, content, "configMap.BinaryData[src.ConfigMapRef.Key]")
		assert.Contains(t, content, "apiSpec.Schema.Value = &resolved")
		assert.Contains(t, content, "payload = flattenSensitiveData(payload)")
		assert.Contains(t, content, "func (obj *FakePlugin) GetConfigMapDataSourceRefs() []ConfigMapDataSourceRef {")
		assert.Contains(t, content, "refs = append(refs, *obj.Spec.APISpec.Schema.ConfigMapRef)")
		assert.NotContains(t, content, "GetSensitiveDataSecretRefs")
		assert.NotContains(t, content, "SensitiveDataSourceTypeSecretRef")
		assert.Contains(t, content, "resolving referenced ConfigMaps via the provided client.")

	})

	t.Run("mixed Secret and ConfigMap references", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			DataSources: map[string][]config.DataSourceConfig{
				"FakePlugin": {
					{Path: "spec.apiSpec.schema", Type: "ConfigMap"},
					{Path: "spec.apiSpec.apiKey", Type: "Secret"},
				},
			},
		})
		content, err := g.generateSDKOps("FakePlugin", schema, opsConfig)
		require.NoError(t, err)

		assert.Contains(t, content, "apiSpec.Schema.Value = &resolved")
		assert.Contains(t, content, "apiSpec.APIKey.Value = &resolved")
		assert.Contains(t, content, "func (obj *FakePlugin) GetConfigMapDataSourceRefs() []ConfigMapDataSourceRef {")
		assert.Contains(t, content, "refs = append(refs, *obj.Spec.APISpec.Schema.ConfigMapRef)")
		assert.NotContains(t, content, "*obj.Spec.APISpec.APIKey.ConfigMapRef")
		assert.Contains(t, content, "func (obj *FakePlugin) GetSensitiveDataSecretRefs() []SensitiveDataSecretRef {")
		assert.Contains(t, content, "refs = append(refs, *obj.Spec.APISpec.APIKey.SecretRef)")
		assert.NotContains(t, content, "*obj.Spec.APISpec.Schema.SecretRef")
		assert.Contains(t, content, "resolving referenced Secrets and ConfigMaps via the provided client.")
	})
}

func TestGenerateCommonTypes_ConfigMapDataSource(t *testing.T) {
	t.Run("emitted when an entity has ConfigMap references", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			DataSources: map[string][]config.DataSourceConfig{
				"FakePlugin": {{Path: "spec.apiSpec.schema", Type: "ConfigMap"}},
			},
		})
		content, err := g.generateCommonTypes(nil)
		require.NoError(t, err)

		assert.Contains(t, content, "type ConfigMapDataSource struct {")
		assert.Contains(t, content, "type ConfigMapDataSourceRef struct {")
		assert.Contains(t, content, `ConfigMapDataSourceTypeConfigMapRef ConfigMapDataSourceType = "configMapRef"`)
		assert.Contains(t, content, "+kubebuilder:validation:Enum=inline;configMapRef")
		assert.Contains(t, content, "+kubebuilder:validation:MaxLength=262144")
		assert.Contains(t, content, `rule="!(has(self.value) && has(self.configMapRef))"`)
		// A ConfigMap-only API group must not emit the Secret-backed type.
		assert.NotContains(t, content, "type SensitiveDataSource struct {")
	})

	t.Run("not emitted for Secret-only references", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			DataSources: map[string][]config.DataSourceConfig{
				"FakeCredential": {{Path: "spec.apiSpec.apiKey", Type: "Secret"}},
			},
		})
		content, err := g.generateCommonTypes(nil)
		require.NoError(t, err)

		assert.Contains(t, content, "type SensitiveDataSource struct {")
		assert.NotContains(t, content, "type ConfigMapDataSource struct {")
		assert.NotContains(t, content, "type ConfigMapDataSourceType string")
	})
}

func TestGenerateWatch_ConfigMapReferences(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
	})

	content, err := g.generateWatch(reconcilerEntityMetadata{
		EntityName:           "FakePlugin",
		EntityNameLowerCamel: "fakePlugin",
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		APIGroupPackageAlias: "konnectv1alpha1",
		HasConfigMapRefs:     true,
	}, &config.ReconcilerConfig{IsRoot: new(true)})
	require.NoError(t, err)

	assert.Contains(t, content, `corev1 "k8s.io/api/core/v1"`)
	assert.Contains(t, content, "&corev1.ConfigMap{}")
	assert.Contains(t, content, "enqueueObjectsForConfigMapRef[konnectv1alpha1.FakePluginList](cl)")
	assert.NotContains(t, content, "&corev1.Secret{}")
}
