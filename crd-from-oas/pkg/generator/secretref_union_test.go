package generator

import (
	"go/format"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// TestBuildSensitiveLeaves_WildcardFanOutAcrossOneOfVariants covers a "*" secret
// reference path on a root-level discriminated union (e.g. AIGatewayModelProvider):
// it must fan out across every variant, recording one selector per variant that
// actually has the field, and silently skip variants that don't.
func TestBuildSensitiveLeaves_WildcardFanOutAcrossOneOfVariants(t *testing.T) {
	fakeBasicSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{
						Name: "auth", Type: "object",
						Properties: []*parser.Property{
							{
								Name: "headers", Type: "array",
								Items: &parser.Property{
									Type: "object",
									Properties: []*parser.Property{
										{Name: "name", Type: "string"},
										{Name: "value", Type: "string"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	// FakeCloud has no "headers" field under config.auth — the wildcard must
	// skip it rather than fail generation.
	fakeCloudSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{Name: "auth", Type: "object", Properties: []*parser.Property{{Name: "type", Type: "string"}}},
				},
			},
		},
	}
	entitySchema := &parser.Schema{
		OneOf: []*parser.Property{
			{RefName: "FakeBasic"},
			{RefName: "FakeCloud"},
		},
		DiscriminatorMapping: map[string]string{
			"fake_basic": "FakeBasic",
			"fake_cloud": "FakeCloud",
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeUnionEntity": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeBasic": fakeBasicSchema,
			"FakeCloud": fakeCloudSchema,
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeUnionEntity": {
				{Path: "spec.apiSpec.*.config.auth.headers[].value", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeUnionEntity")
	require.Len(t, tmpls, 1, "only FakeBasic has the field; FakeCloud must be skipped, not error")
	tmpl := tmpls[0]
	assert.True(t, tmpl.IsSlice)
	assert.Equal(t, "FakeUnionEntityConfig.Basic.Config.Auth.Headers", tmpl.SliceParentSelector)
	assert.Equal(t, "Value", tmpl.SliceLeafField)
	assert.Equal(t, []string{"FakeUnionEntityConfig", "FakeUnionEntityConfig.Basic"}, tmpl.PointerGuards)
}

// TestBuildSensitiveLeaves_WildcardFanOutZeroMatches_Errors ensures a "*" path
// that resolves in NO variant is treated as a config mistake, not silently
// dropped — it must fail generation rather than produce an entity with no
// secret handling at all.
func TestBuildSensitiveLeaves_WildcardFanOutZeroMatches_Errors(t *testing.T) {
	fakeSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", Properties: []*parser.Property{{Name: "name", Type: "string"}}},
		},
	}
	entitySchema := &parser.Schema{
		OneOf:                []*parser.Property{{RefName: "FakeOnly"}},
		DiscriminatorMapping: map[string]string{"fake_only": "FakeOnly"},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeUnionEntity": entitySchema},
		Schemas:       map[string]*parser.Schema{"FakeOnly": fakeSchema},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeUnionEntity": {
				{Path: "spec.apiSpec.*.config.auth.headers[].value", Type: "Secret"},
			},
		},
	})
	err := g.buildSensitiveLeaves(parsed)
	require.Error(t, err)
	assert.ErrorContains(t, err, "matched no variant")
}

// TestBuildSensitiveLeaves_MidPathUnionDescent covers a secret leaf that sits
// behind a discriminated union in the MIDDLE of the path (e.g. a cloud
// provider's "auth" field, which is basic|aws|azure|gcp), as opposed to at the
// entity root. The generated selector must nil-guard both the union container
// field and the selected variant field.
func TestBuildSensitiveLeaves_MidPathUnionDescent(t *testing.T) {
	awsSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "secret_access_key", Type: "string"},
			{Name: "access_key_id", Type: "string"},
		},
	}
	basicSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "headers", Type: "array",
				Items: &parser.Property{Type: "object", Properties: []*parser.Property{{Name: "value", Type: "string"}}},
			},
		},
	}
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{
						Name: "auth", Type: "object",
						OneOf: []*parser.Property{
							{RefName: "FakeAuthBasic"},
							{RefName: "FakeAuthAWS"},
						},
						DiscriminatorMapping: map[string]string{
							"basic": "FakeAuthBasic",
							"aws":   "FakeAuthAWS",
						},
					},
				},
			},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeCloudEntity": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeAuthBasic": basicSchema,
			"FakeAuthAWS":   awsSchema,
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeCloudEntity": {
				{Path: "spec.apiSpec.config.auth.aws.secretAccessKey", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeCloudEntity")
	require.Len(t, tmpls, 1)
	tmpl := tmpls[0]
	assert.False(t, tmpl.IsSlice)
	assert.Equal(t, "Config.Auth.AWS.SecretAccessKey", tmpl.GoFieldSelector)
	assert.Equal(t, []string{"Config.Auth", "Config.Auth.AWS"}, tmpl.PointerGuards)
}

// TestBuildSensitiveLeaves_MidPathUnionDescent_UnknownVariant_Errors ensures a
// typo'd discriminator segment in a mid-path union fails generation instead of
// silently resolving to nothing.
func TestBuildSensitiveLeaves_MidPathUnionDescent_UnknownVariant_Errors(t *testing.T) {
	awsSchema := &parser.Schema{Properties: []*parser.Property{{Name: "secret_access_key", Type: "string"}}}
	entitySchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{
						Name: "auth", Type: "object",
						OneOf:                []*parser.Property{{RefName: "FakeAuthAWS"}},
						DiscriminatorMapping: map[string]string{"aws": "FakeAuthAWS"},
					},
				},
			},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeCloudEntity": entitySchema},
		Schemas:       map[string]*parser.Schema{"FakeAuthAWS": awsSchema},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeCloudEntity": {
				{Path: "spec.apiSpec.config.auth.azure.secretAccessKey", Type: "Secret"},
			},
		},
	})
	err := g.buildSensitiveLeaves(parsed)
	require.Error(t, err)
	assert.ErrorContains(t, err, `variant "azure" not found`)
}

// TestBuildSensitiveLeaves_ArrayOfScalarSecretLeaf covers a secret leaf that is
// itself an array of strings behind a root-level union (mirroring
// AIGatewayIdentityProvider's openid-connect.config.clientSecret): each array
// element IS the SensitiveDataSource, not a per-element field, so the recorded
// selector must be a "self-slice" with an empty SliceLeafField.
func TestBuildSensitiveLeaves_ArrayOfScalarSecretLeaf(t *testing.T) {
	fakeOpenIDConnectSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{Name: "client_id", Type: "array", Items: &parser.Property{Type: "string"}},
					{Name: "client_secret", Type: "array", Items: &parser.Property{Type: "string"}},
				},
			},
		},
	}
	fakeKeyAuthSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", Properties: []*parser.Property{{Name: "key", Type: "string"}}},
		},
	}
	entitySchema := &parser.Schema{
		OneOf: []*parser.Property{
			{RefName: "FakeKeyAuth"},
			{RefName: "FakeOpenIDConnect"},
		},
		DiscriminatorMapping: map[string]string{
			"key-auth":       "FakeKeyAuth",
			"openid-connect": "FakeOpenIDConnect",
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeIdentityProvider": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeKeyAuth":       fakeKeyAuthSchema,
			"FakeOpenIDConnect": fakeOpenIDConnectSchema,
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeIdentityProvider": {
				{Path: "spec.apiSpec.openid-connect.config.clientSecret", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))

	tmpls := g.templateSecretReferences("FakeIdentityProvider")
	require.Len(t, tmpls, 1)
	tmpl := tmpls[0]
	assert.True(t, tmpl.IsSlice)
	assert.Equal(t, "FakeIdentityProviderConfig.OpenIDConnect.Config.ClientSecret", tmpl.SliceParentSelector)
	assert.Empty(t, tmpl.SliceLeafField)
	assert.Equal(t, []string{"FakeIdentityProviderConfig", "FakeIdentityProviderConfig.OpenIDConnect"}, tmpl.PointerGuards)
}

// TestGenerateSDKOps_ArrayOfScalarSecretLeaf_ProducesValidGo is an end-to-end
// regression test for the same shape: it asserts the generated
// sdkOpsAPISpec/GetSensitiveDataSecretRefs compile and access the array
// element directly (no per-element field selector).
func TestGenerateSDKOps_ArrayOfScalarSecretLeaf_ProducesValidGo(t *testing.T) {
	fakeOpenIDConnectSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "config", Type: "object",
				Properties: []*parser.Property{
					{Name: "client_secret", Type: "array", Items: &parser.Property{Type: "string"}},
				},
			},
		},
	}
	fakeKeyAuthSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", Properties: []*parser.Property{{Name: "key", Type: "string"}}},
		},
	}
	entitySchema := &parser.Schema{
		OneOf: []*parser.Property{
			{RefName: "FakeKeyAuth"},
			{RefName: "FakeOpenIDConnect"},
		},
		DiscriminatorMapping: map[string]string{
			"key-auth":       "FakeKeyAuth",
			"openid-connect": "FakeOpenIDConnect",
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeIdentityProvider": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeKeyAuth":       fakeKeyAuthSchema,
			"FakeOpenIDConnect": fakeOpenIDConnectSchema,
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeIdentityProvider": {
				{Path: "spec.apiSpec.openid-connect.config.clientSecret", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateFakeIdentityProviderRequest"},
		},
	}

	content, err := g.generateSDKOps("FakeIdentityProvider", entitySchema, opsConfig)
	require.NoError(t, err)

	_, err = format.Source([]byte(content))
	require.NoError(t, err, "generated code must be valid Go")

	assert.Contains(t, content, "if apiSpec.FakeIdentityProviderConfig != nil {")
	assert.Contains(t, content, "if apiSpec.FakeIdentityProviderConfig.OpenIDConnect != nil {")
	assert.Contains(t, content, "for i := range apiSpec.FakeIdentityProviderConfig.OpenIDConnect.Config.ClientSecret {")
	assert.Contains(t, content, "src := apiSpec.FakeIdentityProviderConfig.OpenIDConnect.Config.ClientSecret[i]\n")
	assert.Contains(t, content, "apiSpec.FakeIdentityProviderConfig.OpenIDConnect.Config.ClientSecret[i].Value = &resolved")

	assert.Contains(t, content, "for _, item := range obj.Spec.APISpec.FakeIdentityProviderConfig.OpenIDConnect.Config.ClientSecret {")
	assert.Contains(t, content, "if item.Type == SensitiveDataSourceTypeSecretRef && item.SecretRef != nil {")
	assert.Contains(t, content, "refs = append(refs, *item.SecretRef)")
}

// TestGenerateSDKOps_WildcardAndMidPathUnion_ProducesValidGo is an end-to-end
// regression test mirroring AIGatewayModelProvider's real shape: a root-level union
// with a "*" secret path across "basic" variants, plus one "cloud" variant
// whose own auth field is a mid-path union (basic arm reachable via the same
// wildcard, native arm reachable via an explicit scalar path). It asserts the
// generated sdkOpsAPISpec/GetSensitiveDataSecretRefs compiles and nil-guards
// every pointer hop.
func TestGenerateSDKOps_WildcardAndMidPathUnion_ProducesValidGo(t *testing.T) {
	basicAuthSchema := &parser.Schema{
		Properties: []*parser.Property{
			{
				Name: "headers", Type: "array",
				Items: &parser.Property{Type: "object", Properties: []*parser.Property{{Name: "value", Type: "string"}}},
			},
		},
	}
	fakeBasicSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", Properties: []*parser.Property{
				{Name: "auth", Type: "object", RefName: "FakeAuthBasic"},
			}},
		},
	}
	awsSchema := &parser.Schema{
		Properties: []*parser.Property{{Name: "secret_access_key", Type: "string"}},
	}
	fakeCloudSchema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "config", Type: "object", Properties: []*parser.Property{
				{
					Name: "auth", Type: "object",
					OneOf: []*parser.Property{
						{RefName: "FakeAuthBasic"},
						{RefName: "FakeAuthAWS"},
					},
					DiscriminatorMapping: map[string]string{
						"basic": "FakeAuthBasic",
						"aws":   "FakeAuthAWS",
					},
				},
			}},
		},
	}
	entitySchema := &parser.Schema{
		OneOf: []*parser.Property{
			{RefName: "FakeBasic"},
			{RefName: "FakeCloud"},
		},
		DiscriminatorMapping: map[string]string{
			"basic_provider": "FakeBasic",
			"cloud":          "FakeCloud",
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{"FakeProvider": entitySchema},
		Schemas: map[string]*parser.Schema{
			"FakeBasic":     fakeBasicSchema,
			"FakeCloud":     fakeCloudSchema,
			"FakeAuthBasic": basicAuthSchema,
			"FakeAuthAWS":   awsSchema,
		},
	}
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		SecretReferences: map[string][]config.SecretReferenceConfig{
			"FakeProvider": {
				{Path: "spec.apiSpec.*.config.auth.headers[].value", Type: "Secret"},
				{Path: "spec.apiSpec.*.config.auth.basic.headers[].value", Type: "Secret"},
				{Path: "spec.apiSpec.cloud.config.auth.aws.secretAccessKey", Type: "Secret"},
			},
		},
	})
	require.NoError(t, g.buildSensitiveLeaves(parsed))
	opsConfig := &config.EntityOpsConfig{
		RequireClient: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateFakeProviderRequest"},
		},
	}

	content, err := g.generateSDKOps("FakeProvider", entitySchema, opsConfig)
	require.NoError(t, err)

	_, err = format.Source([]byte(content))
	require.NoError(t, err, "generated code must be valid Go")

	// Basic variant's wildcard-fanned-out slice leaf, guarded by the container+variant.
	assert.Contains(t, content, "if apiSpec.FakeProviderConfig != nil {")
	assert.Contains(t, content, "if apiSpec.FakeProviderConfig.Basic != nil {")
	assert.Contains(t, content, "for i := range apiSpec.FakeProviderConfig.Basic.Config.Auth.Headers {")

	// Cloud variant's basic arm (mid-path union, reached via the same wildcard).
	assert.Contains(t, content, "for i := range apiSpec.FakeProviderConfig.Cloud.Config.Auth.Basic.Headers {")

	// Cloud variant's native arm (explicit scalar path through the mid-path union).
	assert.Contains(t, content, "if apiSpec.FakeProviderConfig.Cloud.Config.Auth.AWS != nil {")
	assert.Contains(t, content, "src := apiSpec.FakeProviderConfig.Cloud.Config.Auth.AWS.SecretAccessKey")
	assert.Contains(t, content, "apiSpec.FakeProviderConfig.Cloud.Config.Auth.AWS.SecretAccessKey.Value = &resolved")
}
