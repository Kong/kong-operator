package generator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestObjectRefTypeName(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        string
	}{
		{
			name:        "nil commonTypes returns ObjectRef",
			commonTypes: nil,
			want:        "ObjectRef",
		},
		{
			name: "generate true returns ObjectRef",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Generate: new(true),
				},
			},
			want: "ObjectRef",
		},
		{
			name: "import with alias returns qualified name",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
			want: "commonv1alpha1.ObjectRef",
		},
		{
			name: "import without alias uses last path segment",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path: "github.com/kong/kong-operator/v2/api/common/v1alpha1",
					},
				},
			},
			want: "v1alpha1.ObjectRef",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{
				CommonTypes: tc.commonTypes,
			})
			assert.Equal(t, tc.want, g.objectRefTypeName())
		})
	}
}

func TestGoType_ObjectRef(t *testing.T) {
	t.Run("without import uses ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{})
		prop := &parser.Property{IsReference: true}
		assert.Equal(t, "*ObjectRef", g.goType(prop))
	})

	t.Run("with import uses qualified ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		prop := &parser.Property{IsReference: true}
		assert.Equal(t, "*commonv1alpha1.ObjectRef", g.goType(prop))
	})
}

func TestGenerateWatch_UsesStableAPIAuthImportAndNamespacedLookup(t *testing.T) {
	t.Run("reuses generated package import for konnect v1alpha1 entities", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			APIGroupPackageAlias: "konnectv1alpha1",
		})

		content, err := g.generateWatch("Portal")
		require.NoError(t, err)

		assert.Contains(t, content, `konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.NotContains(t, content, `konnectapiauthv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.Contains(t, content, `&konnectv1alpha1.KonnectAPIAuthConfiguration{}`)
	})

	t.Run("uses separate auth import and namespaced lookup for other api groups", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
			APIGroupPackageAlias: "xkonnectv1alpha1",
		})

		content, err := g.generateWatch("Portal")
		require.NoError(t, err)

		assert.Contains(t, content, `konnectapiauthv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"`)
		assert.Contains(t, content, `&konnectapiauthv1alpha1.KonnectAPIAuthConfiguration{}`)
		assert.Contains(t, content, `index.IndexFieldPortalOnAPIAuthConfiguration: auth.Namespace + "/" + auth.Name,`)
	})
}

func TestGenerateIndex_UsesNamespacedAPIAuthKey(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1",
		APIGroupPackageAlias: "xkonnectv1alpha1",
	})

	content, err := g.generateIndex("Portal")
	require.NoError(t, err)

	assert.Contains(t, content, `if ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name == "" {`)
	assert.Contains(t, content, `return []string{ent.GetNamespace() + "/" + ent.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}`)
}

func TestGenerateCommonTypes(t *testing.T) {
	t.Run("without import includes union ObjectRef types", func(t *testing.T) {
		g := NewGenerator(Config{APIVersion: "v1alpha1"})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.Contains(t, content, "type ObjectRefType string")
		assert.Contains(t, content, "type ObjectRef struct")
		assert.Contains(t, content, "Type ObjectRefType")
		assert.NotContains(t, content, "KonnectID")
		assert.Contains(t, content, "NamespacedRef *NamespacedRef")
		assert.Contains(t, content, "type NamespacedRef struct")
		assert.NotContains(t, content, "Namespace *string")
		assert.Contains(t, content, `konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"`)
		assert.Contains(t, content, "type KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus")
		assert.Contains(t, content, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

	t.Run("without import with namespaced includes Namespace field", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Namespaced: true,
				},
			},
		})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.Contains(t, content, "type NamespacedRef struct")
		assert.Contains(t, content, "Namespace *string")
	})

	t.Run("with import excludes ObjectRef types", func(t *testing.T) {
		g := NewGenerator(Config{
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCommonTypes()
		require.NoError(t, err)
		assert.NotContains(t, content, "type ObjectRef struct")
		assert.NotContains(t, content, "type ObjectRefType string")
		assert.NotContains(t, content, "type NamespacedRef struct")
		// Other common types should still be present
		assert.Contains(t, content, "type SecretKeyRef struct")
		assert.Contains(t, content, "type KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus")
		assert.Contains(t, content, "type KonnectEntityRef struct")
		assert.Contains(t, content, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

}

func TestGenerateCRDType_ObjectRefImport(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Dependencies: []*parser.Dependency{
			{
				ParamName:  "portalId",
				EntityName: "Portal",
				FieldName:  "PortalRef",
				JSONName:   "portal_ref",
			},
		},
	}

	t.Run("without import uses unqualified ObjectRef", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "PortalRef ObjectRef")
		assert.NotContains(t, content, "commonv1alpha1")
	})

	t.Run("with import uses qualified ObjectRef and adds import", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCRDType("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "PortalRef commonv1alpha1.ObjectRef")
		assert.Contains(t, content, `commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"`)
	})

	t.Run("with import qualifies ref property types", func(t *testing.T) {
		schemaWithRef := &parser.Schema{
			Name: "CreateTeam",
			Properties: []*parser.Property{
				{
					Name:        "portal_id",
					Type:        "string",
					Format:      "uuid",
					IsReference: true,
				},
			},
		}
		g := NewGenerator(Config{
			APIGroup:   "konnect.konghq.com",
			APIVersion: "v1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
		})
		content, err := g.generateCRDType("CreateTeam", schemaWithRef)
		require.NoError(t, err)
		assert.Contains(t, content, "*commonv1alpha1.ObjectRef")
	})
}

func TestGenerateCRDType_NoObjectRefImportWhenUnneeded(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
		},
	}

	g := NewGenerator(Config{
		APIGroup:   "konnect.konghq.com",
		APIVersion: "v1alpha1",
		CommonTypes: &config.CommonTypesConfig{
			ObjectRef: &config.ObjectRefConfig{
				Import: &config.ImportConfig{
					Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
					Alias: "commonv1alpha1",
				},
			},
		},
	})
	content, err := g.generateCRDType("CreatePortal", schema)
	require.NoError(t, err)
	// When there are no dependencies or ref properties, the import should
	// not be included to avoid unused import errors.
	assert.NotContains(t, content, "commonv1alpha1")
}

func TestGenerateCRDType_DoesNotGenerateHelperMethods(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{
			{
				Name: "labels",
				Type: "object",
				AdditionalProperties: &parser.Property{
					Name: "value",
					Type: "string",
				},
			},
		},
	}

	g := NewGenerator(Config{
		APIGroup:   "x-konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	content, err := g.generateCRDType("CreatePortal", schema)
	require.NoError(t, err)
	assert.NotContains(t, content, "GetKonnectLabels")
	assert.NotContains(t, content, "SetKonnectLabels")
	assert.NotContains(t, content, "GetKonnectStatus")
	assert.NotContains(t, content, "SetKonnectID")
	assert.NotContains(t, content, "konnectv1alpha2")
}

func TestGenerateCRDFuncs_GeneratesKonnectLabelAccessors(t *testing.T) {
	t.Run("referenced labels map type", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name:    "labels",
					Type:    "object",
					RefName: "LabelsUpdate",
					AdditionalProperties: &parser.Property{
						Name: "value",
						Type: "string",
					},
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "func (obj *Portal) GetKonnectLabels() map[string]string {")
		assert.Contains(t, content, "if obj.Spec.APISpec.Labels == nil {")
		assert.Contains(t, content, "labels[key] = string(value)")
		assert.Contains(t, content, "converted := make(LabelsUpdate, len(labels))")
		assert.Contains(t, content, "converted[key] = LabelsUpdateValue(value)")
		assert.Contains(t, content, "obj.Spec.APISpec.Labels = converted")
	})

	t.Run("inline labels map type", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name: "labels",
					Type: "object",
					AdditionalProperties: &parser.Property{
						Name: "value",
						Type: "string",
					},
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, "converted := make(map[string]string, len(labels))")
		assert.Contains(t, content, "converted[key] = string(value)")
	})

	t.Run("without labels field", func(t *testing.T) {
		schema := &parser.Schema{
			Name: "CreatePortal",
			Properties: []*parser.Property{
				{
					Name: "name",
					Type: "string",
				},
			},
		}

		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.NotContains(t, content, "GetKonnectLabels")
		assert.NotContains(t, content, "SetKonnectLabels")
	})
}

func TestGenerateCRDFuncs_GeneratesKonnectFuncs(t *testing.T) {
	schema := &parser.Schema{
		Name: "CreatePortal",
		Properties: []*parser.Property{{
			Name: "name",
			Type: "string",
		}},
	}

	t.Run("default Konnect status return type", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`)
		assert.Contains(t, content, `konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"`)
		assert.Contains(t, content, "func (obj *Portal) GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus {")
		assert.Contains(t, content, "return &obj.Status.KonnectEntityStatus")
		assert.Contains(t, content, "func (obj *Portal) SetKonnectID(id string) {")
		assert.Contains(t, content, "obj.Status.ID = id")
		assert.Contains(t, content, `func (obj *Portal) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj Portal) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *Portal) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *Portal) SetConditions(conditions []metav1.Condition) {`)
	})

	t.Run("reconciler entities include lifecycle helpers in the same file", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"Portal": {
					IsRoot: true,
				},
			},
		})

		content, err := g.generateCRDFuncs("CreatePortal", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`)
		assert.Contains(t, content, `func (obj *Portal) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj Portal) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *Portal) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *Portal) SetConditions(conditions []metav1.Condition) {`)
		assert.Contains(t, content, `func (obj *Portal) GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef {`)
	})

	t.Run("non-root reconciler entities omit auth accessors", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:   "x-konnect.konghq.com",
			APIVersion: "v1alpha1",
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				"PortalTeam": {},
			},
		})

		content, err := g.generateCRDFuncs("CreatePortalTeam", schema)
		require.NoError(t, err)
		assert.Contains(t, content, `func (obj *PortalTeam) GetKonnectID() string {`)
		assert.Contains(t, content, `func (obj PortalTeam) GetTypeName() string {`)
		assert.Contains(t, content, `func (obj *PortalTeam) GetConditions() []metav1.Condition {`)
		assert.Contains(t, content, `func (obj *PortalTeam) SetConditions(conditions []metav1.Condition) {`)
		assert.NotContains(t, content, `GetKonnectAPIAuthConfigurationRef`)
	})
}

func TestGenerate_GeneratesFuncsFile(t *testing.T) {
	g := NewGenerator(Config{
		APIGroup:   "x-konnect.konghq.com",
		APIVersion: "v1alpha1",
	})

	files, err := g.Generate(&parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
				Properties: []*parser.Property{{
					Name: "name",
					Type: "string",
				}},
			},
		},
	})
	require.NoError(t, err)

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name)
	}

	assert.Contains(t, fileNames, "zz_generated_portal_types.go")
	assert.Contains(t, fileNames, "zz_generated_portal_funcs.go")
}

func TestEntityFilePrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "non-Konnect entity",
			input:    "Portal",
			expected: "portal",
		},
		{
			name:     "Konnect-prefixed entity",
			input:    "KonnectEventControlPlane",
			expected: "konnect_eventcontrolplane",
		},
		{
			name:     "Konnect alone stays unchanged",
			input:    "Konnect",
			expected: "konnect",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, entityFilePrefix(tc.input))
		})
	}
}

func TestGenerate_GroupVersionInfo(t *testing.T) {
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
				Properties: []*parser.Property{{
					Name: "name",
					Type: "string",
				}},
			},
		},
	}

	t.Run("enabled by default generates groupversion_info.go", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:                 "x-konnect.konghq.com",
			APIVersion:               "v1alpha1",
			GenerateGroupVersionInfo: true,
		})

		files, err := g.Generate(parsed)
		require.NoError(t, err)

		var fileNames []string
		var gviContent string
		for _, file := range files {
			fileNames = append(fileNames, file.Name)
			if file.Name == "groupversion_info.go" {
				gviContent = file.Content
			}
		}

		assert.Contains(t, fileNames, "groupversion_info.go")
		assert.NotContains(t, fileNames, "register.go")
		assert.Contains(t, gviContent, `GroupVersion = schema.GroupVersion{Group: "x-konnect.konghq.com", Version: "v1alpha1"}`)
		assert.Contains(t, gviContent, "SchemeGroupVersion = GroupVersion")
		assert.Contains(t, gviContent, "func Resource(resource string) schema.GroupResource {")
		assert.Contains(t, gviContent, "Code generated by CRD generation pipeline. DO NOT EDIT.")
	})

	t.Run("disabled skips both registration files", func(t *testing.T) {
		g := NewGenerator(Config{
			APIGroup:                 "konnect.konghq.com",
			APIVersion:               "v1alpha1",
			GenerateGroupVersionInfo: false,
		})

		files, err := g.Generate(parsed)
		require.NoError(t, err)

		var fileNames []string
		for _, file := range files {
			fileNames = append(fileNames, file.Name)
		}

		assert.NotContains(t, fileNames, "groupversion_info.go")
		assert.NotContains(t, fileNames, "register.go")
	})
}

func TestGenerateSchemaTypes_AddsKubebuilderTags(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"CreateDcrProviderRequestOkta": {
				Name: "CreateDcrProviderRequestOkta",
				Properties: []*parser.Property{
					{
						Name: "provider_type",
						Type: "string",
					},
				},
			},
		},
	}

	content := g.generateSchemaTypes(map[string]bool{"CreateDcrProviderRequestOkta": true}, parsed)

	assert.Contains(t, content, "// +optional")
	assert.Contains(t, content, fmt.Sprintf("// +kubebuilder:validation:MaxLength=%d", defaultMaxLength))
	assert.Contains(t, content, "ProviderType string `json:\"provider_type,omitempty\"`")
}

func TestObjectRefNamespaced(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        bool
	}{
		{
			name: "nil commonTypes",
			want: false,
		},
		{
			name:        "nil objectRef",
			commonTypes: &config.CommonTypesConfig{},
			want:        false,
		},
		{
			name: "namespaced false",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Namespaced: false},
			},
			want: false,
		},
		{
			name: "namespaced true",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Namespaced: true},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{CommonTypes: tc.commonTypes})
			assert.Equal(t, tc.want, g.objectRefNamespaced())
		})
	}
}

func TestObjectRefImported(t *testing.T) {
	tests := []struct {
		name        string
		commonTypes *config.CommonTypesConfig
		want        bool
	}{
		{
			name: "nil commonTypes",
			want: false,
		},
		{
			name:        "nil objectRef",
			commonTypes: &config.CommonTypesConfig{},
			want:        false,
		},
		{
			name: "generate true, no import",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{Generate: new(true)},
			},
			want: false,
		},
		{
			name: "import set",
			commonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{Path: "some/path"},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{CommonTypes: tc.commonTypes})
			assert.Equal(t, tc.want, g.objectRefImported())
		})
	}
}

func TestExtractVariantNames(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "single variant - removes common prefixes/suffixes",
			input:    []string{"CreateDcrProviderRequestAuth0"},
			expected: []string{"DcrProviderRequestAuth0"}, // Request not removed since it's not at the end
		},
		{
			name:     "identity provider variants - OIDC and SAML",
			input:    []string{"ConfigureOIDCIdentityProviderConfig", "SAMLIdentityProviderConfig"},
			expected: []string{"OIDC", "SAML"},
		},
		{
			name:     "dcr provider variants - multiple providers",
			input:    []string{"CreateDcrProviderRequestAuth0", "CreateDcrProviderRequestAzureAd", "CreateDcrProviderRequestCurity", "CreateDcrProviderRequestOkta", "CreateDcrProviderRequestHttp"},
			expected: []string{"Auth0", "AzureAd", "Curity", "Okta", "Http"},
		},
		{
			name:     "common prefix only",
			input:    []string{"ConfigTypeA", "ConfigTypeB"},
			expected: []string{"A", "B"},
		},
		{
			name:     "common suffix only",
			input:    []string{"AConfig", "BConfig"},
			expected: []string{"A", "B"},
		},
		{
			name:     "no common prefix or suffix - common suffix 'a' is too short",
			input:    []string{"Alpha", "Beta"},
			expected: []string{"Alph", "Bet"}, // common suffix is "a" so it gets trimmed
		},
		{
			name:     "variants with Configure prefix",
			input:    []string{"ConfigureAuth", "ConfigureSAML"},
			expected: []string{"Auth", "SAML"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractVariantNames(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common prefix",
			a:        "CreateDcrProviderRequestAuth0",
			b:        "CreateDcrProviderRequestAzureAd",
			expected: "CreateDcrProviderRequestA",
		},
		{
			name:     "no common prefix",
			a:        "alpha",
			b:        "beta",
			expected: "",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonPrefix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCommonSuffix(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: "hello",
		},
		{
			name:     "common suffix",
			a:        "ConfigureOIDCIdentityProviderConfig",
			b:        "SAMLIdentityProviderConfig",
			expected: "IdentityProviderConfig",
		},
		{
			name:     "no common suffix",
			a:        "alpha",
			b:        "beta",
			expected: "a",
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: "",
		},
		{
			name:     "one empty string",
			a:        "hello",
			b:        "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := commonSuffix(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCleanSingleVariantName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes Config suffix",
			input:    "SomethingConfig",
			expected: "Something",
		},
		{
			name:     "removes Configuration suffix",
			input:    "SomethingConfiguration",
			expected: "Something",
		},
		{
			name:     "removes Provider suffix",
			input:    "SomethingProvider",
			expected: "Something",
		},
		{
			name:     "removes Request suffix",
			input:    "SomethingRequest",
			expected: "Something",
		},
		{
			name:     "removes Configure prefix",
			input:    "ConfigureSomething",
			expected: "Something",
		},
		{
			name:     "removes Create prefix",
			input:    "CreateSomething",
			expected: "Something",
		},
		{
			name:     "removes Update prefix",
			input:    "UpdateSomething",
			expected: "Something",
		},
		{
			name:     "removes multiple prefixes/suffixes",
			input:    "CreateSomethingRequest",
			expected: "Something",
		},
		{
			name:     "no prefixes or suffixes to remove",
			input:    "Something",
			expected: "Something",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := cleanSingleVariantName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFixInitialisms(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Http becomes HTTP",
			input:    "CreateDcrConfigHttpInRequest",
			expected: "CreateDcrConfigHTTPInRequest",
		},
		{
			name:     "trailing Http becomes HTTP",
			input:    "CreateDcrProviderRequestHttp",
			expected: "CreateDcrProviderRequestHTTP",
		},
		{
			name:     "Url becomes URL",
			input:    "DcrBaseUrl",
			expected: "DcrBaseURL",
		},
		{
			name:     "Api becomes API",
			input:    "DcrConfigPropertyApiKey",
			expected: "DcrConfigPropertyAPIKey",
		},
		{
			name:     "trailing Id becomes ID",
			input:    "DcrConfigPropertyInitialClientId",
			expected: "DcrConfigPropertyInitialClientID",
		},
		{
			name:     "already correct initialisms unchanged",
			input:    "DcrBaseURL",
			expected: "DcrBaseURL",
		},
		{
			name:     "multiple initialisms fixed",
			input:    "HttpApiUrl",
			expected: "HTTPAPIURL",
		},
		{
			name:     "no initialisms unchanged",
			input:    "CreatePortal",
			expected: "CreatePortal",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := fixInitialisms(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSplitPascalCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple PascalCase",
			input:    "CreatePortal",
			expected: []string{"Create", "Portal"},
		},
		{
			name:     "multiple words",
			input:    "CreateDcrConfigHttpInRequest",
			expected: []string{"Create", "Dcr", "Config", "Http", "In", "Request"},
		},
		{
			name:     "single word",
			input:    "Portal",
			expected: []string{"Portal"},
		},
		{
			name:     "existing acronym sequence",
			input:    "DcrBaseURL",
			expected: []string{"Dcr", "Base", "U", "R", "L"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := splitPascalCase(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFormatSchemaComment(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		desc     string
		expected string
	}{
		{
			name:     "empty description",
			typeName: "Labels",
			desc:     "",
			expected: "// Labels is a type alias.\n",
		},
		{
			name:     "description does not start with type name",
			typeName: "Labels",
			desc:     "Store metadata of an entity.",
			expected: "// Labels Store metadata of an entity.\n",
		},
		{
			name:     "description starts with type name - no stutter",
			typeName: "Labels",
			desc:     "Labels store metadata of an entity.",
			expected: "// Labels store metadata of an entity.\n",
		},
		{
			name:     "description starts with type name followed by dot",
			typeName: "Labels",
			desc:     "Labels.",
			expected: "// Labels.\n",
		},
		{
			name:     "description equals type name exactly",
			typeName: "Labels",
			desc:     "Labels",
			expected: "// Labels\n",
		},
		{
			name:     "trailing empty lines are stripped",
			typeName: "Labels",
			desc:     "Labels store metadata.\n\n",
			expected: "// Labels store metadata.\n",
		},
		{
			name:     "multiline with trailing empty lines stripped",
			typeName: "Labels",
			desc:     "Labels store metadata.\n\nKeys must be 1-63 chars.\n\n",
			expected: "// Labels store metadata.\n//\n// Keys must be 1-63 chars.\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatSchemaComment(tc.typeName, tc.desc)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCollectSDKOpsBoolFields(t *testing.T) {
	g := NewGenerator(Config{})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "enabled",
				Type: "boolean",
			},
			{
				Name: "settings",
				Type: "object",
				Properties: []*parser.Property{
					{
						Name: "nested_enabled",
						Type: "boolean",
					},
				},
			},
			{
				Name: "items",
				Type: "array",
				Items: &parser.Property{
					Type: "object",
					Properties: []*parser.Property{
						{
							Name: "item_enabled",
							Type: "boolean",
						},
					},
				},
			},
			{
				Name: "flags",
				Type: "object",
				AdditionalProperties: &parser.Property{
					Type: "boolean",
				},
			},
		},
	}

	assert.Equal(t, []sdkOpsBoolField{
		{Label: "enabled", Path: []string{"enabled"}},
		{Label: "flags.{}", Path: []string{"flags", "{}"}},
		{Label: "items.[].item_enabled", Path: []string{"items", "[]", "item_enabled"}},
		{Label: "settings.nested_enabled", Path: []string{"settings", "nested_enabled"}},
	}, g.collectSDKOpsBoolFields(schema))
}

func TestGenerateSDKOps_NormalizesBooleanFields(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
			{
				Name: "rbac_enabled",
				Type: "boolean",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			},
		},
	}

	content, err := g.generateSDKOps("Portal", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, "var sdkOpsBoolFields = []sdkOpsBoolField")
	assert.Contains(t, content, "func (s *PortalAPISpec) marshalSDKOpsPayload() ([]byte, error)")
	assert.Contains(t, content, "data, err := s.marshalSDKOpsPayload()")
	assert.Contains(t, content, "Label: \"rbac_enabled\"")
	assert.Contains(t, content, "failed to normalize PortalAPISpec SDK payload")
	assert.Contains(t, content, "case \"Enabled\":")
	assert.Contains(t, content, "return true, nil")
	assert.NotContains(t, content, "error) {\n\n\tdata")
	assert.NotContains(t, content, "err)\n\n\tvar target")
	assert.NotContains(t, content, "}var target")
	assert.Contains(t, content, "}\n\tvar target")
	assert.NotContains(t, content, "if err := normalizeSDKOpsBoolFields(payload); err != nil {\n\t\treturn nil, fmt.Errorf(\"failed to normalize PortalAPISpec for CreatePortal: %w\", err)")
}

func TestGenerateSDKOpsTest_AssertsNormalizedPayload(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "name",
				Type: "string",
			},
			{
				Name: "rbac_enabled",
				Type: "boolean",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			},
		},
	}

	content, err := g.generateSDKOpsTest("Portal", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `RBACEnabled: "Enabled"`)
	assert.Contains(t, content, `require.Equal(t, true, payload["rbac_enabled"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["name"])`)
}

func TestGenerateSDKOpsTest_SupportsPointerAndNamedFields(t *testing.T) {
	g := NewGenerator(Config{APIVersion: "v1alpha1"})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name:     "description",
				Type:     "string",
				Nullable: true,
			},
			{
				Name:    "labels",
				Type:    "object",
				RefName: "Labels",
				AdditionalProperties: &parser.Property{
					Name:    "value",
					Type:    "string",
					RefName: "LabelsValue",
				},
			},
			{
				Name:    "min_runtime_version",
				Type:    "string",
				RefName: "MinRuntimeVersion",
			},
			{
				Name:    "name",
				Type:    "string",
				RefName: "GatewayName",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {
				Path: "github.com/Kong/sdk-konnect-go/models/components.CreateGatewayRequest",
			},
		},
	}

	content, err := g.generateSDKOpsTest("KonnectEventControlPlane", schema, opsConfig)
	require.NoError(t, err)
	assert.Contains(t, content, `Description: new("test-value")`)
	assert.Contains(t, content, `Labels: Labels{"test-key": "test-value"}`)
	assert.Contains(t, content, `MinRuntimeVersion: MinRuntimeVersion("test-value")`)
	assert.Contains(t, content, `Name: GatewayName("test-value")`)
	assert.Contains(t, content, `require.Equal(t, map[string]any{"test-key": "test-value"}, payload["labels"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["min_runtime_version"])`)
	assert.Contains(t, content, `require.Equal(t, "test-value", payload["name"])`)
}

func TestParseSDKTypePath(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantImport string
		wantType   string
		wantErr    bool
	}{
		{
			name:       "valid SDK type path",
			input:      "github.com/Kong/sdk-konnect-go/models/components.CreatePortal",
			wantImport: "github.com/Kong/sdk-konnect-go/models/components",
			wantType:   "CreatePortal",
		},
		{
			name:       "valid path with nested packages",
			input:      "github.com/Kong/sdk-konnect-go/models/operations.ListPortals",
			wantImport: "github.com/Kong/sdk-konnect-go/models/operations",
			wantType:   "ListPortals",
		},
		{
			name:    "no dot separator",
			input:   "noDotAtAll",
			wantErr: true,
		},
		{
			name:    "leading dot",
			input:   ".CreatePortal",
			wantErr: true,
		},
		{
			name:    "trailing dot",
			input:   "github.com/Kong/sdk-konnect-go/models/components.",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			importPath, typeName, err := ParseSDKTypePath(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantImport, importPath)
			assert.Equal(t, tc.wantType, typeName)
		})
	}
}

func TestGenerateSchemaTypes_MapWithValueTypes(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"Labels": {
				Name:          "Labels",
				Description:   "Labels store metadata.",
				Type:          "object",
				MaxProperties: func() *int64 { v := int64(50); return &v }(),
				AdditionalProperties: &parser.Property{
					Type:      "string",
					MinLength: func() *int64 { v := int64(1); return &v }(),
					MaxLength: func() *int64 { v := int64(63); return &v }(),
					Pattern:   `^[a-z0-9A-Z]+$`,
				},
			},
			"LabelsUpdate": {
				Name:        "LabelsUpdate",
				Description: "LabelsUpdate store metadata.",
				Type:        "object",
				AdditionalProperties: &parser.Property{
					Type:      "string",
					MinLength: func() *int64 { v := int64(1); return &v }(),
					MaxLength: func() *int64 { v := int64(63); return &v }(),
					Pattern:   `^[a-z0-9A-Z]+$`,
				},
			},
		},
	}

	refs := map[string]bool{
		"Labels":       true,
		"LabelsUpdate": true,
	}

	content := g.generateSchemaTypes(refs, parsed)

	// Labels should generate a value type with native markers, then a map type using it
	assert.Contains(t, content, "type LabelsValue string")
	assert.Contains(t, content, "type Labels map[string]LabelsValue")
	assert.Contains(t, content, "+kubebuilder:validation:MinLength=1")
	assert.Contains(t, content, "+kubebuilder:validation:MaxLength=63")
	assert.Contains(t, content, "+kubebuilder:validation:Pattern=`^[a-z0-9A-Z]+$`")

	// LabelsUpdate should also generate a value type
	assert.Contains(t, content, "type LabelsUpdateValue string")
	assert.Contains(t, content, "type LabelsUpdate map[string]LabelsUpdateValue")

	// No CEL XValidation rules or MaxProperties on the type (goes on the field)
	assert.NotContains(t, content, "XValidation")
	assert.NotContains(t, content, "MaxProperties")
}

func TestGenerateSchemaTypes_NoValueTypeForNonMapTypes(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
	})

	parsed := &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			"GatewayName": {
				Name:        "GatewayName",
				Description: "The name of the Gateway.",
				Type:        "string",
			},
		},
	}

	refs := map[string]bool{
		"GatewayName": true,
	}

	content := g.generateSchemaTypes(refs, parsed)

	assert.Contains(t, content, "type GatewayName string")
	assert.NotContains(t, content, "Value")
	assert.NotContains(t, content, "XValidation")
	assert.NotContains(t, content, "MaxProperties")
}
