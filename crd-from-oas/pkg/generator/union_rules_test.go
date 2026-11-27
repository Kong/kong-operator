package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestEmitDiscriminatedUnionCode_UnionMemberRules(t *testing.T) {
	variants := []unionVariant{
		{discValue: "sasl_plain", fieldName: "SaslPlain", goTypeName: "SaslPlain", requiresMember: true},
		{discValue: "anonymous", fieldName: "Anonymous", goTypeName: "Anonymous"},
	}

	on := emitDiscriminatedUnionCode("Auth", "auth", "type", variants, config.ValidationConfig{UnionMemberRules: true})
	assert.Contains(t, on, `+kubebuilder:validation:XValidation:rule="self.type == 'saslPlain' ? has(self.saslPlain) : !has(self.saslPlain)",message="saslPlain must be set when type is saslPlain, and only then"`,
		"a member with required fields must be set")
	assert.Contains(t, on, `+kubebuilder:validation:XValidation:rule="!has(self.anonymous) || self.type == 'anonymous'",message="anonymous must be set only when type is anonymous"`,
		"a member without required fields may be left out")

	off := emitDiscriminatedUnionCode("Auth", "auth", "type", variants, config.ValidationConfig{})
	assert.NotContains(t, off, "XValidation", "not opted in")

	listed := emitDiscriminatedUnionCode("EncryptionKey", "key", "type", variants, config.ValidationConfig{})
	assert.Contains(t, listed, `rule="self.type == 'anonymous' ? has(self.anonymous) : !has(self.anonymous)"`,
		"the types listed in shouldEmitTypeMatchCEL keep their rules")
}

func TestUnionMemberHasRequiredFields(t *testing.T) {
	g := NewGenerator(Config{})
	g.parsed = &parser.ParsedSpec{Schemas: map[string]*parser.Schema{
		"WithRequired":  {Required: []string{"type", "username"}},
		"OnlyType":      {Required: []string{"type"}},
		"RequiredProps": {Properties: []*parser.Property{{Name: "op", Required: true}, {Name: "key", Required: true}}},
		"PathsArray":    {Type: "array"},
		"Expression":    {Type: "string"},
		"EmptyObject":   {Type: "object"},
	}}
	assert.True(t, g.unionMemberHasRequiredFields("PathsArray", nil, "type"), "an array member is the value itself")
	assert.True(t, g.unionMemberHasRequiredFields("Expression", nil, "type"), "a string member is the value itself")
	assert.False(t, g.unionMemberHasRequiredFields("EmptyObject", nil, "type"))
	assert.True(t, g.unionMemberHasRequiredFields("", &parser.Property{Type: "string"}, "type"))
	assert.True(t, g.unionMemberHasRequiredFields("WithRequired", nil, "type"))
	assert.False(t, g.unionMemberHasRequiredFields("OnlyType", nil, "type"), "the discriminator doesn't count")
	assert.True(t, g.unionMemberHasRequiredFields("RequiredProps", nil, "op"))
	assert.False(t, g.unionMemberHasRequiredFields("", &parser.Property{Properties: []*parser.Property{{Name: "type", Required: true}}}, ""),
		"the discriminator defaults to type")
	assert.True(t, g.unionMemberHasRequiredFields("", &parser.Property{Properties: []*parser.Property{{Name: "phrase", Required: true}}}, "type"))
}

func TestSDKUnionDiscriminators(t *testing.T) {
	assert.Equal(t, []string{"type", "op", "schemaType", "schema_type", "outputFormat", "output_format", "URLType", "url_type"},
		sdkUnionDiscriminatorKeys([]string{"type", "op", "schemaType", "outputFormat", "URLType"}))

	g := NewGenerator(Config{SDKUnionDiscriminators: []string{"type"}})
	assert.NotPanics(t, func() { g.checkSDKUnionDiscriminator("Auth", "type") })
	assert.PanicsWithValue(t, `union Rule uses the discriminator "op", which sdkUnionDiscriminators of its API group doesn't list`,
		func() { g.checkSDKUnionDiscriminator("Rule", "op") })
	assert.NotPanics(t, func() { NewGenerator(Config{}).checkSDKUnionDiscriminator("Rule", "op") }, "no list: every discriminator")

	listed, err := NewGenerator(Config{APIVersion: "v1alpha1", SDKUnionDiscriminators: []string{"type", "outputFormat"}}).generateCommonTypes(nil)
	require.NoError(t, err)
	assert.Contains(t, listed, `var sdkUnionDiscriminatorKeys = []string{"type", "outputFormat", "output_format"}`)
	assert.Contains(t, listed, "for _, key := range sdkUnionDiscriminatorKeys {")
	assert.NotContains(t, listed, "for key := range object {", "no fallback to any key")

	unlisted, err := NewGenerator(Config{APIVersion: "v1alpha1"}).generateCommonTypes(nil)
	require.NoError(t, err)
	assert.Contains(t, unlisted, `preferred := []string{"type", "op", "kind", "mode"}`)
	assert.Contains(t, unlisted, "for key := range object {")
	assert.NotContains(t, unlisted, "sdkUnionDiscriminatorKeys")
}
