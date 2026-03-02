package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

//go:fix inline
func ptrTo[T any](v T) *T { return new(v) }

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

func TestGenerateCommonTypes(t *testing.T) {
	t.Run("without import includes ObjectRef types", func(t *testing.T) {
		g := NewGenerator(Config{APIVersion: "v1alpha1"})
		content := g.generateCommonTypes()
		assert.Contains(t, content, "type ObjectRef struct")
		assert.Contains(t, content, "type NamespacedObjectRef struct")
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
		content := g.generateCommonTypes()
		assert.NotContains(t, content, "type ObjectRef struct")
		assert.NotContains(t, content, "type NamespacedObjectRef struct")
		// Other common types should still be present
		assert.Contains(t, content, "type SecretKeyRef struct")
		assert.Contains(t, content, "type KonnectEntityStatus struct")
		assert.Contains(t, content, "type KonnectEntityRef struct")
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
