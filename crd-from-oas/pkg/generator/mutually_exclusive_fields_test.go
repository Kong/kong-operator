package generator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func mutuallyExclusiveFieldsSpec() *parser.ParsedSpec {
	return &parser.ParsedSpec{
		Schemas: map[string]*parser.Schema{
			// Both fields optional: receives the marker.
			"TargetAlphaConfig": {
				Name: "TargetAlphaConfig",
				Properties: []*parser.Property{
					{Name: "upstream_url", Type: "string"},
					{Name: "upstream_base_url", Type: "string"},
				},
			},
			// Missing upstream_base_url: skipped.
			"TargetBetaConfig": {
				Name: "TargetBetaConfig",
				Properties: []*parser.Property{
					{Name: "upstream_url", Type: "string"},
				},
			},
			// upstream_url required: skipped.
			"TargetGammaConfig": {
				Name:     "TargetGammaConfig",
				Required: []string{"upstream_url"},
				Properties: []*parser.Property{
					{Name: "upstream_url", Type: "string", Required: true},
					{Name: "upstream_base_url", Type: "string"},
				},
			},
			// Has both fields but doesn't match the pattern: skipped.
			"OtherConfig": {
				Name: "OtherConfig",
				Properties: []*parser.Property{
					{Name: "upstream_url", Type: "string"},
					{Name: "upstream_base_url", Type: "string"},
				},
			},
		},
	}
}

func allSchemaRefs(parsed *parser.ParsedSpec) map[string]bool {
	refs := make(map[string]bool, len(parsed.Schemas))
	for name := range parsed.Schemas {
		refs[name] = true
	}
	return refs
}

func TestGenerateSchemaTypes_MutuallyExclusiveFields(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		MutuallyExclusiveFields: []config.MutuallyExclusiveFieldsConfig{{
			Schemas: []string{"Target*Config"},
			Fields:  []string{"upstreamURL", "upstreamBaseURL"},
		}},
	})
	parsed := mutuallyExclusiveFieldsSpec()

	content := g.generateSchemaTypes(allSchemaRefs(parsed), parsed, nil)

	const marker = "// +kubebuilder:validation:AtMostOneOf=upstreamURL;upstreamBaseURL\n"
	assert.Contains(t, content, "//\n"+marker+"type TargetAlphaConfig struct {")
	assert.Equal(t, 1, strings.Count(content, marker), "only TargetAlphaConfig must get the marker")

	require.NoError(t, g.validateMutuallyExclusiveFields(allSchemaRefs(parsed), parsed))
}

func TestGenerateSchemaTypes_MutuallyExclusiveFieldsMultipleRules(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		MutuallyExclusiveFields: []config.MutuallyExclusiveFieldsConfig{
			{Schemas: []string{"TargetAlphaConfig"}, Fields: []string{"upstreamURL", "upstreamBaseURL"}},
			{Schemas: []string{"OtherConfig", "TargetAlpha*"}, Fields: []string{"upstreamBaseURL", "upstreamURL"}},
		},
	})
	parsed := mutuallyExclusiveFieldsSpec()

	content := g.generateSchemaTypes(allSchemaRefs(parsed), parsed, nil)

	assert.Contains(t, content, "//\n"+
		"// +kubebuilder:validation:AtMostOneOf=upstreamURL;upstreamBaseURL\n"+
		"// +kubebuilder:validation:AtMostOneOf=upstreamBaseURL;upstreamURL\n"+
		"type TargetAlphaConfig struct {")
	assert.Contains(t, content, "//\n"+
		"// +kubebuilder:validation:AtMostOneOf=upstreamBaseURL;upstreamURL\n"+
		"type OtherConfig struct {")
}

func TestValidateMutuallyExclusiveFields_UnusedRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule config.MutuallyExclusiveFieldsConfig
	}{
		{
			name: "no schema matches the pattern",
			rule: config.MutuallyExclusiveFieldsConfig{Schemas: []string{"Missing*"}, Fields: []string{"upstreamURL", "upstreamBaseURL"}},
		},
		{
			name: "matched schemas lack a field",
			rule: config.MutuallyExclusiveFieldsConfig{Schemas: []string{"Target*Config"}, Fields: []string{"upstreamURL", "renamedURL"}},
		},
		{
			name: "matched schema requires a field",
			rule: config.MutuallyExclusiveFieldsConfig{Schemas: []string{"TargetGammaConfig"}, Fields: []string{"upstreamURL", "upstreamBaseURL"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGenerator(Config{
				APIVersion:              "v1alpha1",
				MutuallyExclusiveFields: []config.MutuallyExclusiveFieldsConfig{tc.rule},
			})
			parsed := mutuallyExclusiveFieldsSpec()

			err := g.validateMutuallyExclusiveFields(allSchemaRefs(parsed), parsed)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "mutuallyExclusiveFields[0]")
		})
	}
}

func TestValidateMutuallyExclusiveFields_OnlyReferencedSchemas(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		MutuallyExclusiveFields: []config.MutuallyExclusiveFieldsConfig{{
			Schemas: []string{"TargetAlphaConfig"},
			Fields:  []string{"upstreamURL", "upstreamBaseURL"},
		}},
	})
	parsed := mutuallyExclusiveFieldsSpec()

	// TargetAlphaConfig exists in the spec but isn't emitted.
	err := g.validateMutuallyExclusiveFields(map[string]bool{"TargetBetaConfig": true}, parsed)
	require.Error(t, err)
}
