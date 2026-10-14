package generator

import (
	"go/format"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func parentPolicyGenerator(t *testing.T) *Generator {
	t.Helper()
	const entity = "EventGatewayVirtualClusterProducePolicy"
	variant := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "parent_policy_id", Type: "string", Required: true},
			{Name: "name", Type: "string"},
			{Name: "labels", Type: "object"},
		},
	}
	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			entity: {
				OneOf: []*parser.Property{
					{RefName: "EventGatewayParsedRecordEncryptFieldsPolicyCreate", Properties: variant.Properties},
				},
				DiscriminatorMapping: map[string]string{"encrypt_fields": "EventGatewayParsedRecordEncryptFieldsPolicyCreate"},
			},
		},
		Schemas: map[string]*parser.Schema{"EventGatewayParsedRecordEncryptFieldsPolicyCreate": variant},
	}
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		entity: {{
			Path: "spec.apiSpec.parentPolicyRef", Kinds: []string{entity}, ResolvesTo: "id",
			InjectInto: "parentPolicyID", RequiredAPISpecType: "schemaValidation", ReverseWatch: true,
		}},
	})
	g.config.SchemaFieldOmissions = map[string]map[string]bool{
		"EventGatewayParsedRecordEncryptFieldsPolicyCreate": {"parentPolicyID": true},
	}
	g.config.APIGroupPackagePath = "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	g.config.APIGroupPackageAlias = "configurationv1alpha1"
	return g
}

func TestRootUnionInjectIntoReference(t *testing.T) {
	const entity = "EventGatewayVirtualClusterProducePolicy"
	t.Run("target must be omitted from the variants", func(t *testing.T) {
		g := parentPolicyGenerator(t)
		g.config.SchemaFieldOmissions = nil
		require.ErrorContains(t, g.addInjectIntoReferenceProperties(g.parsed), "must be omitted")
	})
	t.Run("generates sibling JSON handling and SDK injection", func(t *testing.T) {
		g := parentPolicyGenerator(t)
		require.NoError(t, g.addInjectIntoReferenceProperties(g.parsed))
		schema := g.parsed.RequestBodies[entity]
		require.Len(t, schema.Properties, 1)
		assert.Equal(t, "parent_policy_ref", schema.Properties[0].Name)
		assert.False(t, schema.Properties[0].Required)
		hasTags, hasLabels, _ := metadataFields(schema, nil)
		assert.False(t, hasTags)
		assert.True(t, hasLabels, "sibling reference fields must not hide variant metadata")
		assert.True(t, schemaHasNameProperty(schema, nil))
		content, err := g.generateCRDType(entity, schema)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(content, "func (s EventGatewayVirtualClusterProducePolicyAPISpec) MarshalJSON"))
		assert.Equal(t, 1, strings.Count(content, "func (s *EventGatewayVirtualClusterProducePolicyAPISpec) UnmarshalJSON"))
		assert.Contains(t, content, `payload["parentPolicyRef"] = data`)
		assert.Contains(t, content, `!has(self.spec.apiSpec) || !has(self.spec.apiSpec.parentPolicyRef)`)
		assert.NotContains(t, content, "exactly one of")
		content, err = g.generateSDKOps(entity, schema, &config.EntityOpsConfig{
			Ops: map[string]*config.OpConfig{
				"create": {Path: "github.com/Kong/sdk-konnect-go/models/operations.CreateEventGatewayVirtualClusterProducePolicyRequest"},
			},
		})
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)
		assert.Contains(t, content, `payload["parent_policy_id"] = resolvedParentPolicyRef[0]`)
		assert.Contains(t, content, `selectedMap["parent_policy_id"] = value`)
		assert.Contains(t, content, `must have apiSpec.type schemaValidation`)
		assert.Empty(t, g.literalTargetIndexes(entity))
	})
	t.Run("merges same-kind forward and reverse watches", func(t *testing.T) {
		g := parentPolicyGenerator(t)
		require.NoError(t, g.addInjectIntoReferenceProperties(g.parsed))
		content, err := g.generateWatch(reconcilerEntityMetadata{
			EntityName: entity, EntityNameLowerCamel: "eventGatewayVirtualClusterProducePolicy",
			ParentEntityName: "EventGatewayVirtualCluster", ParentRefFieldName: "EventGatewayVirtualClusterRef",
			APIGroupPackagePath: g.config.APIGroupPackagePath, APIGroupPackageAlias: g.config.APIGroupPackageAlias,
			ParentAPIGroupPackagePath: g.config.APIGroupPackagePath, ParentAPIGroupPackageAlias: g.config.APIGroupPackageAlias,
		}, &config.ReconcilerConfig{IsRoot: new(false), ParentEntityType: "EventGatewayVirtualCluster"})
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(content, "func enqueueEventGatewayVirtualClusterProducePolicyForEventGatewayVirtualClusterProducePolicy("))
		assert.Contains(t, content, "RefsToEventGatewayVirtualClusterProducePolicy(ref)")
		assert.NotContains(t, content, "referrer.Spec.APISpec.ParentPolicyID")
	})
}

// policyParsedSpec builds a fixture whose AIGatewayPolicy schema mirrors the
// relevant part of the real one: a required string "type" that an additive
// reference injects into.
func policyParsedSpec() *parser.ParsedSpec {
	return &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"AIGatewayPolicy": {
				Properties: []*parser.Property{
					{Name: "name", Type: "string", Required: true},
					{Name: "type", Type: "string", Required: true},
				},
				Required: []string{"name", "type"},
			},
		},
		Schemas: map[string]*parser.Schema{},
	}
}

func customPolicyRefConfig() config.ReferenceConfig {
	return config.ReferenceConfig{
		Path:        "spec.apiSpec.customPolicyRef",
		Kinds:       []string{"AIGatewayCustomPolicy"},
		ResolvesTo:  "name",
		InjectInto:  "type",
		Description: "CustomPolicyRef references the custom policy.",
	}
}

func TestAddInjectIntoReferenceProperties(t *testing.T) {
	t.Run("adds the reference field and makes the target optional", func(t *testing.T) {
		parsed := policyParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayPolicy": {customPolicyRefConfig()},
		})

		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))

		schema := parsed.RequestBodies["AIGatewayPolicy"]
		assert.Equal(t, []string{"name"}, schema.Required)

		target := findAPISpecProperty(parsed, "AIGatewayPolicy", "type")
		require.NotNil(t, target)
		assert.False(t, target.Required)
		require.NotNil(t, target.MinLength, "the target keeps the MinLength it had as a required string")
		assert.Equal(t, int64(1), *target.MinLength)

		field := findAPISpecProperty(parsed, "AIGatewayPolicy", "customPolicyRef")
		require.NotNil(t, field)
		assert.Equal(t, "custom_policy_ref", field.Name)
		assert.Equal(t, "string", field.Type)
		assert.False(t, field.Required)
		assert.Equal(t, "CustomPolicyRef references the custom policy.", field.Description)
	})

	t.Run("an explicit minLength of 0 on the target is raised to 1", func(t *testing.T) {
		parsed := policyParsedSpec()
		parsed.RequestBodies["AIGatewayPolicy"].Properties[1].MinLength = new(int64(0))
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayPolicy": {customPolicyRefConfig()},
		})

		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))
		target := findAPISpecProperty(parsed, "AIGatewayPolicy", "type")
		require.NotNil(t, target)
		require.NotNil(t, target.MinLength)
		assert.Equal(t, int64(1), *target.MinLength, "an empty target must not satisfy the exactly-one rule")
	})

	t.Run("two references injecting into the same target are rejected", func(t *testing.T) {
		parsed := policyParsedSpec()
		other := customPolicyRefConfig()
		other.Path = "spec.apiSpec.otherPolicyRef"
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayPolicy": {customPolicyRefConfig(), other},
		})

		require.ErrorContains(t, g.addInjectIntoReferenceProperties(parsed), `both inject into "type"`)
	})

	errorCases := []struct {
		name    string
		mutate  func(*parser.ParsedSpec)
		ref     config.ReferenceConfig
		wantErr string
	}{
		{
			name: "target missing",
			ref: func() config.ReferenceConfig {
				ref := customPolicyRefConfig()
				ref.InjectInto = "kind"
				return ref
			}(),
			wantErr: `injectInto target "kind" not found`,
		},
		{
			name: "reference field already in the OAS",
			mutate: func(parsed *parser.ParsedSpec) {
				schema := parsed.RequestBodies["AIGatewayPolicy"]
				schema.Properties = append(schema.Properties, &parser.Property{Name: "custom_policy_ref", Type: "string"})
			},
			ref:     customPolicyRefConfig(),
			wantErr: `injectInto field "customPolicyRef" already exists`,
		},
		{
			name: "non-string target",
			mutate: func(parsed *parser.ParsedSpec) {
				parsed.RequestBodies["AIGatewayPolicy"].Properties[1].Type = "object"
			},
			ref:     customPolicyRefConfig(),
			wantErr: "must be a plain, non-nullable string field",
		},
		{
			name: "named string target",
			mutate: func(parsed *parser.ParsedSpec) {
				parsed.RequestBodies["AIGatewayPolicy"].Properties[1].RefName = "PolicyType"
			},
			ref:     customPolicyRefConfig(),
			wantErr: "must be a plain, non-nullable string field",
		},
		{
			name: "nullable target",
			mutate: func(parsed *parser.ParsedSpec) {
				parsed.RequestBodies["AIGatewayPolicy"].Properties[1].Nullable = true
			},
			ref:     customPolicyRefConfig(),
			wantErr: "must be a plain, non-nullable string field",
		},
		{
			name: "root-union entity",
			mutate: func(parsed *parser.ParsedSpec) {
				parsed.RequestBodies["AIGatewayPolicy"].OneOf = []*parser.Property{{Name: "variant"}}
			},
			ref:     customPolicyRefConfig(),
			wantErr: "not found in root-union variants",
		},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			parsed := policyParsedSpec()
			if tc.mutate != nil {
				tc.mutate(parsed)
			}
			g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
				"AIGatewayPolicy": {tc.ref},
			})
			require.ErrorContains(t, g.addInjectIntoReferenceProperties(parsed), tc.wantErr)
		})
	}
}

func TestInjectIntoReferenceXValidations(t *testing.T) {
	assert.Equal(t,
		[]string{
			`+kubebuilder:validation:XValidation:rule="!has(self.spec) || !has(self.spec.apiSpec) || has(self.spec.apiSpec.type) != has(self.spec.apiSpec.customPolicyRef)", message="exactly one of spec.apiSpec.type and spec.apiSpec.customPolicyRef must be set"`,
		},
		injectIntoReferenceXValidations([]config.ReferenceConfig{
			{Path: "spec.apiSpec.policies", Kinds: []string{"AIGatewayPolicy"}, ResolvesTo: "name"},
			customPolicyRefConfig(),
		}),
	)
}

func TestGenerateSDKOps_InjectIntoReference(t *testing.T) {
	parsed := policyParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayPolicy": {customPolicyRefConfig()},
	})
	require.NoError(t, g.addInjectIntoReferenceProperties(parsed))
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayPolicyRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayPolicyRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayPolicy", parsed.RequestBodies["AIGatewayPolicy"], opsConfig)
	require.NoError(t, err)

	// An unset reference yields nothing to resolve.
	require.Contains(t, content, "func RefsAtAIGatewayPolicyCustomPolicyRef(obj *AIGatewayPolicy) []AIGatewayCustomPolicyRef {")
	require.Contains(t, content, `if obj.Spec.APISpec.CustomPolicyRef.Name == "" {`)
	require.Contains(t, content, "refs := RefsAtAIGatewayPolicyCustomPolicyRef(obj)")
	require.Contains(t, content, "resolved = append(resolved, referenced.GetKonnectName())")
	// The reference's own key never reaches Konnect; its resolved value is
	// sent as the target field.
	require.Contains(t, content, `delete(payload, "custom_policy_ref")`)
	require.Contains(t, content, `payload["type"] = resolvedCustomPolicyRef[0]`)
	require.NotContains(t, content, `payload["custom_policy_ref"] =`)
}

// TestTemplateReferences_InjectIntoUsesOASPropertyName verifies that the
// resolved value is injected under the target's OAS property name even when
// converting its CRD JSON name back would not produce it (a camelCase OAS
// name like "policyType" would otherwise become "policy_type").
func TestTemplateReferences_InjectIntoUsesOASPropertyName(t *testing.T) {
	parsed := policyParsedSpec()
	parsed.RequestBodies["AIGatewayPolicy"].Properties[1].Name = "policyType"
	ref := customPolicyRefConfig()
	ref.InjectInto = "policyType"
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayPolicy": {ref},
	})
	require.NoError(t, g.addInjectIntoReferenceProperties(parsed))

	refs := g.templateReferences("AIGatewayPolicy")
	require.Len(t, refs, 1)
	assert.True(t, refs[0].OptionalRef)
	assert.Equal(t, "policyType", refs[0].InjectIntoSDKJSONFieldName)
	assert.Equal(t, "custom_policy_ref", refs[0].SDKJSONFieldName)
}

func TestGenerateEntityOpsTestFile_AssertsInjectInto(t *testing.T) {
	opsCfg := &config.EntityOpsConfig{
		SDK: &config.OpSDKConfig{
			Interface: "github.com/Kong/sdk-konnect-go.AIGatewayPoliciesSDK",
			FieldName: "AIGatewayPolicies",
		},
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayPolicyRequest"},
		},
	}
	generate := func(t *testing.T, ref config.ReferenceConfig) string {
		t.Helper()
		parsed := policyParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}})
		g.config.APIGroupPackageAlias = "aiconfigurationv1alpha1"
		g.config.APIGroupPackagePath = "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))
		schema := parsed.RequestBodies["AIGatewayPolicy"]
		schema.OperationID = "create-ai-gateway-policy"
		schema.SuccessResponseRef = "AIGatewayPolicy"
		schema.Tags = []string{"ai-gateway-policies"}
		res, err := g.generateEntityOpsFile("AIGatewayPolicy", schema, opsCfg)
		require.NoError(t, err)
		require.NotNil(t, res.TestFile)
		_, err = format.Source([]byte(res.TestFile.Content))
		require.NoError(t, err)
		return res.TestFile.Content
	}

	t.Run("references resolving to an ID assert the injected Konnect ID", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ResolvesTo = "id"
		content := generate(t, ref)
		assert.Contains(t, content, `"encoding/json"`)
		assert.Contains(t, content, "data, err := json.Marshal(expectedRequest)")
		assert.Contains(t, content, "var referenced0 aiconfigurationv1alpha1.AIGatewayCustomPolicy")
		assert.Contains(t, content, `require.Equal(t, referenced0.GetKonnectID(), body["type"])`)
	})
}
