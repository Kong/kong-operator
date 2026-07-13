package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// TestBuildSensitiveLeaves_DirectField_NonString covers a direct apiSpec-level
// secret reference field (e.g. AIGatewayPolicy.config) whose OAS type is a
// free-form JSON object rather than string: it must get a dedicated per-field
// type instead of the shared SensitiveDataSource.
func TestBuildSensitiveLeaves_DirectField_NonString(t *testing.T) {
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", AdditionalProperties: &parser.Property{Type: "string"}},
			{Name: "name", Type: "string"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakePolicy": entitySchema},
		Schemas:       map[string]*parser.Schema{},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakePolicy": {
				{Path: "spec.apiSpec.config", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakePolicy")
	require.Len(t, tmpls, 1)
	tmpl := tmpls[0]
	assert.Equal(t, "map[string]string", tmpl.ValueGoType)
	assert.Equal(t, "FakePolicyConfigDataSource", tmpl.DedicatedTypeName)
	assert.Equal(t, "Config", tmpl.GoFieldSelector)

	content, err := g.generateCRDType("FakePolicy", entitySchema)
	require.NoError(t, err)
	assert.Contains(t, content, "Config FakePolicyConfigDataSource `json:\"config,omitzero\"`")
	assert.Contains(t, content, "type FakePolicyConfigDataSource struct {")
	assert.Contains(t, content, "Value *map[string]string `json:\"value,omitempty\"`")
}

// TestBuildSensitiveLeaves_DirectField_StringStillUsesSharedType is a
// regression guard: a plain string direct field must keep resolving to the
// shared SensitiveDataSource type, with no dedicated type generated.
func TestBuildSensitiveLeaves_DirectField_StringStillUsesSharedType(t *testing.T) {
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "apiKey", Type: "string"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeCredential": entitySchema},
		Schemas:       map[string]*parser.Schema{},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeCredential": {
				{Path: "spec.apiSpec.apiKey", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeCredential")
	require.Len(t, tmpls, 1)
	assert.Equal(t, "string", tmpls[0].ValueGoType)
	assert.Empty(t, tmpls[0].DedicatedTypeName)

	content, err := g.generateCRDType("FakeCredential", entitySchema)
	require.NoError(t, err)
	assert.Contains(t, content, "APIKey SensitiveDataSource")
	assert.NotContains(t, content, "DataSource struct")
}

// TestBuildSensitiveLeaves_NestedSchemaField_NonString covers a nested
// (non-direct) secret reference leaf inside a $ref'd schema whose OAS type is
// an integer: the containing schema's field must use a dedicated type.
func TestBuildSensitiveLeaves_NestedSchemaField_NonString(t *testing.T) {
	tlsSchema := &parser.Schema{
		Name: "FakeTLS",
		Properties: []*parser.Property{
			{Name: "port", Type: "integer"},
		},
	}
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "tls", RefName: "FakeTLS"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeListener": entitySchema},
		Schemas:       map[string]*parser.Schema{"FakeTLS": tlsSchema},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeListener": {
				{Path: "spec.apiSpec.tls.port", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeListener")
	require.Len(t, tmpls, 1)
	assert.Equal(t, "int", tmpls[0].ValueGoType)
	assert.Equal(t, "FakeListenerTLSPortDataSource", tmpls[0].DedicatedTypeName)

	lt, ok := g.schemaFieldSensitiveType("FakeTLS", "port")
	require.True(t, ok)
	assert.Equal(t, "FakeListenerTLSPortDataSource", lt.DedicatedTypeName)
}

// TestBuildSensitiveLeaves_RefToScalarAliasSchema_ResolvesUnderlyingType is a
// regression guard for a real-world shape (Kong's GatewaySecret/
// GatewaySecretReferenceOrLiteral): a $ref to a schema that's itself just a
// plain "type: string" alias must resolve to "string" — not to the alias's Go
// type name — so it keeps using the shared SensitiveDataSource.
func TestBuildSensitiveLeaves_RefToScalarAliasSchema_ResolvesUnderlyingType(t *testing.T) {
	aliasSchema := &parser.Schema{Name: "FakeSecretAlias", Type: "string"}
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "certificate", RefName: "FakeSecretAlias"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeCertHolder": entitySchema},
		Schemas:       map[string]*parser.Schema{"FakeSecretAlias": aliasSchema},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeCertHolder": {
				{Path: "spec.apiSpec.certificate", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeCertHolder")
	require.Len(t, tmpls, 1)
	assert.Equal(t, "string", tmpls[0].ValueGoType)
	assert.Empty(t, tmpls[0].DedicatedTypeName)
}

// TestBuildSensitiveLeaves_UnionLeaf_Errors ensures a secret reference path
// that resolves to a oneOf union (no single value shape) fails generation
// with a clear error instead of silently misrendering.
func TestBuildSensitiveLeaves_UnionLeaf_Errors(t *testing.T) {
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "auth", Type: "object",
				OneOf:                []*parser.Property{{RefName: "FakeAuthA"}, {RefName: "FakeAuthB"}},
				DiscriminatorMapping: map[string]string{"a": "FakeAuthA", "b": "FakeAuthB"},
			},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeEntity": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeAuthA": {Properties: []*parser.Property{{Name: "token", Type: "string"}}},
			"FakeAuthB": {Properties: []*parser.Property{{Name: "token", Type: "string"}}},
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeEntity": {
				{Path: "spec.apiSpec.auth", Type: "Secret"},
			},
		},
	})
	err := g.buildSensitiveLeaves(parsed)
	require.Error(t, err)
	assert.ErrorContains(t, err, "union")
}

// TestBuildSensitiveLeaves_NestedObjectLeaf_Errors ensures a secret reference
// path resolving to an object with declared nested properties (a struct, not
// a single value) fails generation with a clear error.
func TestBuildSensitiveLeaves_NestedObjectLeaf_Errors(t *testing.T) {
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "identity", Type: "object",
				Properties: []*parser.Property{{Name: "certificate", Type: "string"}},
			},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeEntity": entitySchema},
		Schemas:       map[string]*parser.Schema{},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeEntity": {
				{Path: "spec.apiSpec.identity", Type: "Secret"},
			},
		},
	})
	err := g.buildSensitiveLeaves(parsed)
	require.Error(t, err)
	assert.ErrorContains(t, err, "nested properties")
}

// TestGenerateSDKOps_NonStringSecretReference_CallsManualResolver verifies
// that the generated sdkOpsAPISpec routes a non-string secret reference leaf
// through the hand-written valueFromSecretRef method instead of inlining the
// (string-only) Secret-fetch-and-convert logic.
func TestGenerateSDKOps_NonStringSecretReference_CallsManualResolver(t *testing.T) {
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", AdditionalProperties: &parser.Property{Type: "string"}},
			{Name: "name", Type: "string"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakePolicy": entitySchema},
		Schemas:       map[string]*parser.Schema{},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakePolicy": {
				{Path: "spec.apiSpec.config", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateFakePolicyRequest"},
		},
	}
	content, err := g.generateSDKOps("FakePolicy", entitySchema, opsConfig)
	require.NoError(t, err)

	assert.Contains(t, content, "resolved, err := src.valueFromSecretRef(ctx, cl, namespace)")
	assert.Contains(t, content, "valueFromSecretRef is hand-written for FakePolicyConfigDataSource")
	assert.Contains(t, content, "apiSpec.Config.Value = &resolved")
	assert.NotContains(t, content, "secretBytes, ok := secret.Data[src.SecretRef.Key]")
}
