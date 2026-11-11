package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// registryUnionMemberParsedSpec has an entity whose config holds two fields of
// a by-id/by-name reference object rendered as a tagged union: a $ref to a
// oneOf of $refs without discriminator, like SchemaRegistryReference.
func registryUnionMemberParsedSpec() *parser.ParsedSpec {
	byID := &parser.Property{RefName: "RegistryReferenceById", Properties: []*parser.Property{{Name: "id", Type: "string"}}}
	byName := &parser.Property{RefName: "RegistryReferenceByName", Properties: []*parser.Property{{Name: "name", Type: "string"}}}
	return &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"AIGatewayAgent": {Properties: []*parser.Property{
				{Name: "config", Type: "object", Properties: []*parser.Property{
					{Name: "schema_registry", Type: "object", Required: true, RefName: "RegistryReference", OneOf: []*parser.Property{byID, byName}},
					{Name: "backup_registry", Type: "object", RefName: "RegistryReference", OneOf: []*parser.Property{byID, byName}},
				}},
			}},
		},
		Schemas: map[string]*parser.Schema{
			"RegistryReference":       {OneOf: []*parser.Property{byID, byName}},
			"RegistryReferenceById":   {Properties: byID.Properties},
			"RegistryReferenceByName": {Properties: byName.Properties},
		},
	}
}

func registryRef(path, unionMember string) config.ReferenceConfig {
	return config.ReferenceConfig{
		Path:        path,
		Kinds:       []string{"AIGatewayConsumerGroup"},
		ResolvesTo:  "name",
		UnionMember: unionMember,
	}
}

func TestValidateReferences_TaggedUnionMember(t *testing.T) {
	const (
		schemaRegistry = "spec.apiSpec.config.schemaRegistry"
		backupRegistry = "spec.apiSpec.config.backupRegistry"
	)

	t.Run("accepts a member on every field of the reference object", func(t *testing.T) {
		parsed := registryUnionMemberParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {registryRef(schemaRegistry, "namespacedRef"), registryRef(backupRegistry, "namespacedRef")},
		})
		require.NoError(t, g.validateReferences(parsed))
	})

	t.Run("counts a field replaced by a reference as covered", func(t *testing.T) {
		parsed := registryUnionMemberParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {registryRef(schemaRegistry, "namespacedRef"), registryRef(backupRegistry, "")},
		})
		require.NoError(t, g.validateReferences(parsed))
	})

	t.Run("rejects a field of the reference object without a reference", func(t *testing.T) {
		parsed := registryUnionMemberParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {registryRef(schemaRegistry, "namespacedRef")},
		})
		require.ErrorContains(t, g.validateReferences(parsed), "embeds it in 2 field(s) with 1 matching references entries")
	})

	for _, member := range []string{"type", "id", "name"} {
		t.Run("rejects a member colliding with "+member, func(t *testing.T) {
			parsed := registryUnionMemberParsedSpec()
			g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
				"AIGatewayAgent": {registryRef(schemaRegistry, member), registryRef(backupRegistry, member)},
			})
			require.ErrorContains(t, g.validateReferences(parsed), "collides with field")
		})
	}

	t.Run("rejects a oneOf with a discriminator", func(t *testing.T) {
		parsed := registryUnionMemberParsedSpec()
		parsed.Schemas["RegistryReference"].Discriminator = "kind"
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {registryRef(schemaRegistry, "namespacedRef"), registryRef(backupRegistry, "namespacedRef")},
		})
		require.ErrorContains(t, g.validateReferences(parsed), "oneOf of $refs without discriminator")
	})
}

func TestGenerateUnionType_TaggedUnionMember(t *testing.T) {
	parsed := registryUnionMemberParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {
			registryRef("spec.apiSpec.config.schemaRegistry", "namespacedRef"),
			registryRef("spec.apiSpec.config.backupRegistry", "namespacedRef"),
		},
	})
	prop := parsed.RequestBodies["AIGatewayAgent"].Properties[0].Properties[0]

	code := g.generateUnionType(prop, "AIGatewayAgentConfig")

	assert.Contains(t, code, "+kubebuilder:validation:Enum=id;name;namespacedRef")
	assert.Contains(t, code, "NamespacedRef *AIGatewayConsumerGroupRef `json:\"namespacedRef,omitempty\"`")
	assert.Contains(t, code, "NamespacedRef references an in-cluster AIGatewayConsumerGroup object")
	assert.Contains(t, code, `case "namespacedRef":`, "UnmarshalJSON decodes the member")
	assert.Contains(t, code, `m["namespacedRef"] = raw`, "MarshalJSON encodes the member")
}

func TestRefFieldTarget_TaggedUnionMemberLeafIsPointer(t *testing.T) {
	parsed := registryUnionMemberParsedSpec()
	ref := registryRef("spec.apiSpec.config.schemaRegistry", "namespacedRef")
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{"AIGatewayAgent": {ref}})

	_, _, goPath, err := g.refFieldTarget("AIGatewayAgent", ref)
	require.NoError(t, err)
	require.NotEmpty(t, goPath)
	leaf := goPath[len(goPath)-1]
	assert.True(t, leaf.ObjectRefLeaf)
	assert.True(t, leaf.Pointer, "a required field keeping its tagged union holds a pointer to the union type")
}

func TestUnionArmChecks_DiscriminatorName(t *testing.T) {
	segs := []GoPathSegment{
		{Name: "Config", Pointer: true, UnionWrapper: true, UnionTypeName: "TranscodeConfig", UnionDiscriminatorGoName: "OutputFormat"},
		{Name: "Avro", Pointer: true, UnionVariant: true},
		{Name: "Settings", Pointer: true, UnionWrapper: true, UnionTypeName: "Settings"},
		{Name: "Static", Pointer: true, UnionVariant: true},
	}

	assert.Equal(t, []string{
		"obj.Config.OutputFormat != TranscodeConfigTypeAvro",
		"obj.Config.Avro.Settings.Type != SettingsTypeStatic",
	}, unionArmChecks("obj.", segs))
}

func TestGenerateReferences_TypedMember(t *testing.T) {
	const (
		schemaRegistry = "spec.apiSpec.config.schemaRegistry"
		backupRegistry = "spec.apiSpec.config.backupRegistry"
	)
	typed := func(path string) config.ReferenceConfig {
		ref := registryRef(path, "")
		ref.TypedMember = "namespacedRef"
		return ref
	}
	parsed := registryUnionMemberParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {typed(schemaRegistry), typed(backupRegistry)},
	})
	require.NoError(t, g.validateReferences(parsed))

	t.Run("the field holds the type accepting both forms", func(t *testing.T) {
		fields := g.schemaTypeRefFields()
		var found []string
		for _, byField := range fields {
			for _, typeName := range byField {
				found = append(found, typeName)
			}
		}
		assert.Contains(t, found, "AIGatewayConsumerGroupRefWithNamespacedRef")
		assert.NotContains(t, found, "AIGatewayConsumerGroupRef")
	})

	t.Run("the type keeps the ref struct fields and adds the tagged member", func(t *testing.T) {
		out, err := g.GenerateReferencesFile()
		require.NoError(t, err)
		for _, want := range []string{
			"type AIGatewayConsumerGroupRefWithNamespacedRef struct {",
			"NamespacedRef *AIGatewayConsumerGroupRef `json:\"namespacedRef,omitempty\"`",
			"Name string `json:\"name,omitempty\"`",
			"// +kubebuilder:validation:Enum=namespacedRef",
			`rule="has(self.name) != has(self.namespacedRef)"`,
			`rule="has(self.type) == has(self.namespacedRef)"`,
			"func (r AIGatewayConsumerGroupRefWithNamespacedRef) Ref() AIGatewayConsumerGroupRef {",
			// The ref struct itself is still generated, for the member.
			"type AIGatewayConsumerGroupRef struct {",
		} {
			assert.Contains(t, out, want)
		}
	})
}

func TestShouldSkipSchemaProperty_ReferenceByID(t *testing.T) {
	g := newTestGeneratorWithParsed(t, registryUnionMemberParsedSpec(), nil)
	id := &parser.Property{Name: "id", Type: "string"}
	assert.False(t, g.shouldSkipSchemaProperty("SchemaRegistryReferenceByID", id), "a by-id reference holds its id")
	assert.True(t, g.shouldSkipSchemaProperty("EventGatewaySchemaRegistry", id), "the id of an entity is the server's")
	assert.True(t, g.shouldSkipSchemaProperty("SchemaRegistryReferenceByID", &parser.Property{Name: "id", Type: "string", ReadOnly: true}))
}
