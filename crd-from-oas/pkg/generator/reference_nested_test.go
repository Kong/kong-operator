package generator

import (
	"go/format"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// agentModelParsedSpec builds a fixture whose AIGatewayAgent schema mirrors the
// real one: access (object AIGatewayAgentAccess) -> acls (oneOf union) ->
// allow (object AIGatewayAllowACL) -> allow (array of string). It also includes
// an AIGatewayModel entity that embeds the same shared AIGatewayAgentAccess type
// via api (AIGatewayModelAPI), so both entities reach AIGatewayAllowACL.
//
// access also carries a throttle field ($ref to AIGatewayThrottle), an
// anyOf-registered schema (schema.AnyOf non-empty), used to exercise the
// pointer segment for $ref-to-anyOf-schema hops in refFieldTarget's walk.
func agentModelParsedSpec() *parser.ParsedSpec {
	allowACL := &parser.Schema{Properties: []*parser.Property{
		{Name: "allow", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	denyACL := &parser.Schema{Properties: []*parser.Property{
		{Name: "deny", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	aclsUnion := &parser.Property{
		Name:          "acls",
		Discriminator: "type",
		OneOf: []*parser.Property{
			{RefName: "AIGatewayAllowACL"},
			{RefName: "AIGatewayDenyACL"},
		},
		DiscriminatorMapping: map[string]string{
			"allow": "AIGatewayAllowACL",
			"deny":  "AIGatewayDenyACL",
		},
	}
	throttle := &parser.Schema{
		// Registers this schema as anyOf (schema.AnyOf non-empty), which is
		// what g.anyOfSchemaNames keys off of.
		AnyOf: []*parser.Property{
			{RefName: "AIGatewayThrottleFixed"},
			{RefName: "AIGatewayThrottleSliding"},
		},
		Properties: []*parser.Property{
			{Name: "limits", Type: "array", Items: &parser.Property{Type: "string"}},
		},
	}
	access := &parser.Schema{Properties: []*parser.Property{
		aclsUnion,
		{Name: "throttle", Type: "object", RefName: "AIGatewayThrottle"},
	}}
	agentAPISpec := &parser.Schema{Properties: []*parser.Property{
		{Name: "access", Type: "object", RefName: "AIGatewayAgentAccess"},
		{Name: "policies", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	modelAPI := &parser.Schema{Properties: []*parser.Property{
		{Name: "access", Type: "object", RefName: "AIGatewayAgentAccess"},
	}}
	modelAPISpec := &parser.Schema{Properties: []*parser.Property{
		{Name: "api", Type: "object", RefName: "AIGatewayModelAPI"},
	}}
	return &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"AIGatewayAgent": agentAPISpec,
			"AIGatewayModel": modelAPISpec,
		},
		Schemas: map[string]*parser.Schema{
			"AIGatewayAgentAccess": access,
			"AIGatewayAllowACL":    allowACL,
			"AIGatewayDenyACL":     denyACL,
			"AIGatewayModelAPI":    modelAPI,
			"AIGatewayThrottle":    throttle,
		},
	}
}

func newTestGeneratorWithParsed(t *testing.T, parsed *parser.ParsedSpec, refs map[string][]config.ReferenceConfig) *Generator {
	t.Helper()
	g := NewGenerator(Config{APIVersion: "v1alpha1", References: refs})
	g.parsed = parsed
	g.ensureInlineTypeNames(parsed)
	// Mirror Generate()'s pre-computation of anyOf-registered schema names so
	// tests observe the same pointer-rendering decisions as real generation.
	g.anyOfSchemaNames = make(map[string]bool)
	for name, schema := range parsed.Schemas {
		if len(schema.AnyOf) > 0 {
			g.anyOfSchemaNames[name] = true
		}
	}
	return g
}

func TestRefFieldTarget(t *testing.T) {
	parsed := agentModelParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, nil)

	// Nested path through object -> union -> variant -> array leaf.
	typeName, field, goPath, err := g.refFieldTarget("AIGatewayAgent", config.ReferenceConfig{
		Path: "spec.apiSpec.access.acls.allow.allow",
	})
	require.NoError(t, err)
	require.Equal(t, "AIGatewayAllowACL", typeName)
	require.Equal(t, "allow", field)
	require.Equal(t, []GoPathSegment{
		{Name: "Access", Pointer: false, JSONKey: "access"},
		{Name: "Acls", Pointer: true, JSONKey: "acls", UnionWrapper: true, UnionTypeName: "AIGatewayAgentAccessAcls"},
		{Name: "Allow", Pointer: true, JSONKey: "allow", UnionVariant: true, VariantProperties: 1},
		{Name: "Allow", Pointer: false, JSONKey: "allow"},
	}, goPath)

	// Nested path through object -> $ref to an anyOf-registered schema ->
	// array leaf. The $ref segment (throttle) must be marked Pointer: true
	// since AIGatewayThrottle is anyOf-registered and its generated Go field
	// is therefore a pointer (see goTypeInCRD's
	// `prop.RefName != "" && g.anyOfSchemaNames[prop.RefName]` case).
	typeName, field, goPath, err = g.refFieldTarget("AIGatewayAgent", config.ReferenceConfig{
		Path: "spec.apiSpec.access.throttle.limits",
	})
	require.NoError(t, err)
	require.Equal(t, "AIGatewayThrottle", typeName)
	require.Equal(t, "limits", field)
	require.Equal(t, []GoPathSegment{
		{Name: "Access", Pointer: false, JSONKey: "access"},
		{Name: "Throttle", Pointer: true, JSONKey: "throttle"},
		{Name: "Limits", Pointer: false, JSONKey: "limits"},
	}, goPath)

	// Top-level fields resolve to the entity APISpec itself.
	typeName, field, _, err = g.refFieldTarget("AIGatewayAgent", config.ReferenceConfig{
		Path: "spec.apiSpec.policies",
	})
	require.NoError(t, err)
	require.Equal(t, "AIGatewayAgentAPISpec", typeName)
	require.Equal(t, "policies", field)

	// Unknown segment errors with the offending path.
	_, _, _, err = g.refFieldTarget("AIGatewayAgent", config.ReferenceConfig{ //nolint:dogsled // only the error matters here
		Path: "spec.apiSpec.access.nope",
	})
	require.ErrorContains(t, err, "spec.apiSpec.access.nope")

	// A non-array final property is a config error.
	_, _, _, err = g.refFieldTarget("AIGatewayAgent", config.ReferenceConfig{ //nolint:dogsled // only the error matters here
		Path: "spec.apiSpec.access",
	})
	require.ErrorContains(t, err, "spec.apiSpec.access")
}

func TestReferenceForFieldOnlyMatchesTopLevelAPISpecFields(t *testing.T) {
	parsed := agentModelParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {
			{
				Path:        "spec.apiSpec.access.acls.allow.allow",
				Kinds:       []string{"AIGatewayConsumerGroup"},
				ResolvesTo:  "name",
				RefTypeName: "AIGatewayACLRef",
			},
			{
				Path:       "spec.apiSpec.policies",
				Kinds:      []string{"AIGatewayPolicy"},
				ResolvesTo: "id",
			},
		},
	})

	require.Nil(t, g.referenceForField("AIGatewayAgent", "allow"))
	require.NotNil(t, g.referenceForField("AIGatewayAgent", "policies"))
	require.NotContains(t, g.schemaTypeRefFields(), "AIGatewayAgentAPISpec")
	require.Contains(t, g.schemaTypeRefFields(), "AIGatewayAllowACL")
}

// TestGenerateSchemaTypes_RefifiesSharedSchemaFields verifies that a nested
// reference (through object -> union -> variant -> array leaf) causes the owning
// shared schema type's leaf field to be emitted as a slice of the generated ref
// struct instead of the OAS-derived item type, while preserving array-level
// bounds and suppressing item-level string markers.
func TestGenerateSchemaTypes_RefifiesSharedSchemaFields(t *testing.T) {
	parsed := agentModelParsedSpec()
	// Give the allow leaf a MaxItems bound so we can assert it is preserved.
	maxItems := int64(8)
	parsed.Schemas["AIGatewayAllowACL"].Properties[0].MaxItems = &maxItems

	aclRef := config.ReferenceConfig{
		Path:        "spec.apiSpec.access.acls.allow.allow",
		Kinds:       []string{"AIGatewayConsumerGroup"},
		ResolvesTo:  "name",
		RefTypeName: "AIGatewayACLRef",
	}
	denyRef := aclRef
	denyRef.Path = "spec.apiSpec.access.acls.deny.deny"

	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {aclRef, denyRef},
		"AIGatewayModel": {
			{Path: "spec.apiSpec.api.access.acls.allow.allow", Kinds: aclRef.Kinds, ResolvesTo: "name", RefTypeName: "AIGatewayACLRef"},
			{Path: "spec.apiSpec.api.access.acls.deny.deny", Kinds: aclRef.Kinds, ResolvesTo: "name", RefTypeName: "AIGatewayACLRef"},
		},
	})

	out := g.generateSchemaTypes(map[string]bool{
		"AIGatewayAllowACL": true,
		"AIGatewayDenyACL":  true,
	}, parsed, nil)

	require.Contains(t, out, "Allow []AIGatewayACLRef `json:\"allow,omitempty\"`")
	require.Contains(t, out, "Deny []AIGatewayACLRef `json:\"deny,omitempty\"`")
	require.NotContains(t, out, "Allow []string")
	require.NotContains(t, out, "Deny []string")
	// Array-level bound preserved.
	require.Contains(t, out, "+kubebuilder:validation:MaxItems=8")
}

// TestGenerateSDKOps_NestedSingleKindNameResolver verifies the resolver
// generated for a nested, single-kind, resolvesTo:name reference: it is named by
// the full Go path, defaults the kind, and resolves each reference to the
// Konnect name.
func TestGenerateSDKOps_NestedSingleKindNameResolver(t *testing.T) {
	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
		References: map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {
				{
					Path:        "spec.apiSpec.access.acls.allow.allow",
					Kinds:       []string{"AIGatewayConsumerGroup"},
					ResolvesTo:  "name",
					RefTypeName: "AIGatewayACLRef",
				},
			},
		},
	})
	schema := &parser.Schema{
		Properties: []*parser.Property{
			{Name: "access", Type: "object"},
			{Name: "name", Type: "string"},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayAgentRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayAgentRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayAgent", schema, opsConfig)
	require.NoError(t, err)

	require.Contains(t, content, "func resolveAIGatewayAgentAccessAclsAllowAllow(ctx context.Context, cl client.Client, obj *AIGatewayAgent) ([]string, error)")
	// Nested refs source through the generated nil-guarded accessor.
	require.Contains(t, content, "refs := RefsAtAIGatewayAgentAccessAclsAllowAllow(obj)")
	// Single-kind references default empty kind to AIGatewayConsumerGroup and do
	// not emit multi-kind dispatch.
	require.Contains(t, content, `kind = "AIGatewayConsumerGroup"`)
	require.NotContains(t, content, "switch kind {")
	// resolvesTo:name selects the Konnect name as the resolved value.
	require.Contains(t, content, "string(referenced.Spec.APISpec.Name)")
	// Programmed check applies in name mode too.
	require.Contains(t, content, `ReferenceNotProgrammedError{Kind: "AIGatewayConsumerGroup", Namespace: ns, Name: ref.Name}`)
}

// TestGenerateSDKOps_NestedRefAccessor verifies that a nested reference emits a
// nil-guarded refsAt accessor: one "if ... == nil { return nil }" per pointer
// segment of the Go field-access chain (the union field and the selected
// variant field), followed by a direct return of the leaf slice.
func TestGenerateSDKOps_NestedRefAccessor(t *testing.T) {
	parsed := agentModelParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {
			{
				Path:        "spec.apiSpec.access.acls.allow.allow",
				Kinds:       []string{"AIGatewayConsumerGroup"},
				ResolvesTo:  "name",
				RefTypeName: "AIGatewayACLRef",
			},
		},
	})
	schema := parsed.RequestBodies["AIGatewayAgent"]
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayAgentRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayAgentRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayAgent", schema, opsConfig)
	require.NoError(t, err)

	require.Contains(t, content, "func RefsAtAIGatewayAgentAccessAclsAllowAllow(obj *AIGatewayAgent) []AIGatewayACLRef {")
	require.Contains(t, content, "if obj.Spec.APISpec.Access.Acls == nil {")
	require.Contains(t, content, "if obj.Spec.APISpec.Access.Acls.Allow == nil {")
	require.Contains(t, content, "return obj.Spec.APISpec.Access.Acls.Allow.Allow")
	// The accessor is used by the resolver as the refs source.
	require.Contains(t, content, "refs := RefsAtAIGatewayAgentAccessAclsAllowAllow(obj)")
}

// TestGenerateSDKOps_NestedRefAccessor_AnyOfRefSegment verifies that a nested
// reference path traversing a $ref property whose target schema is
// anyOf-registered emits a nil guard for that segment too (the field is a
// pointer at that hop, mirroring goTypeInCRD's rendering), not just for
// oneOf/anyOf union-wrapper and variant segments.
func TestGenerateSDKOps_NestedRefAccessor_AnyOfRefSegment(t *testing.T) {
	parsed := agentModelParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": {
			{
				Path:        "spec.apiSpec.access.throttle.limits",
				Kinds:       []string{"AIGatewayConsumer", "AIGatewayConsumerGroup"},
				ResolvesTo:  "name",
				RefTypeName: "AIGatewayThrottleLimitRef",
			},
		},
	})
	schema := parsed.RequestBodies["AIGatewayAgent"]
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayAgentRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayAgentRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayAgent", schema, opsConfig)
	require.NoError(t, err)

	require.Contains(t, content, "func RefsAtAIGatewayAgentAccessThrottleLimits(obj *AIGatewayAgent) []AIGatewayThrottleLimitRef {")
	// The throttle segment must be nil-guarded: AIGatewayThrottle is
	// anyOf-registered, so obj.Spec.APISpec.Access.Throttle is a *AIGatewayThrottle.
	require.Contains(t, content, "if obj.Spec.APISpec.Access.Throttle == nil {")
	require.Contains(t, content, "return obj.Spec.APISpec.Access.Throttle.Limits")
}

// TestValidateReferences_EmbedderConsistency ensures that when a shared schema
// type field is referenced-typed via one entity, every other entity whose
// schema reaches that same type must declare a matching reference entry.
func TestValidateReferences_EmbedderConsistency(t *testing.T) {
	parsed := agentModelParsedSpec()

	aclRef := config.ReferenceConfig{
		Path:        "spec.apiSpec.access.acls.allow.allow",
		Kinds:       []string{"AIGatewayConsumerGroup"},
		ResolvesTo:  "id",
		RefTypeName: "AIGatewayACLRef",
	}

	t.Run("agent declares, model does not", func(t *testing.T) {
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {aclRef},
		})
		err := g.validateReferences(parsed)
		require.Error(t, err)
		require.ErrorContains(t, err, "AIGatewayAllowACL")
		require.ErrorContains(t, err, "AIGatewayModel")
	})

	t.Run("both declare matching entries", func(t *testing.T) {
		modelRef := aclRef
		modelRef.Path = "spec.apiSpec.api.access.acls.allow.allow"
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {aclRef},
			"AIGatewayModel": {modelRef},
		})
		require.NoError(t, g.validateReferences(parsed))
	})
}

// modelRootUnionParsedSpec builds a fixture mirroring the real AIGatewayModel:
// the entity request body is a root-level discriminated union (api | model)
// whose "api" variant embeds access -> acls (oneOf union) -> allow/deny ->
// array leaf. access also carries an identity_providers sibling field, which
// SDK payload injection must preserve.
func modelRootUnionParsedSpec() *parser.ParsedSpec {
	allowACL := &parser.Schema{Properties: []*parser.Property{
		{Name: "allow", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	denyACL := &parser.Schema{Properties: []*parser.Property{
		{Name: "deny", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	aclsUnion := &parser.Property{
		Name:          "acls",
		Discriminator: "type",
		OneOf: []*parser.Property{
			{RefName: "AIGatewayAllowACL"},
			{RefName: "AIGatewayDenyACL"},
		},
		DiscriminatorMapping: map[string]string{
			"allow": "AIGatewayAllowACL",
			"deny":  "AIGatewayDenyACL",
		},
	}
	modelAccess := &parser.Schema{Properties: []*parser.Property{
		aclsUnion,
		{Name: "identity_providers", Type: "array", Items: &parser.Property{Type: "string"}},
	}}
	modelAPI := &parser.Schema{Properties: []*parser.Property{
		{Name: "access", Type: "object", RefName: "AIGatewayModelAccess"},
		{Name: "name", Type: "string"},
	}}
	modelModel := &parser.Schema{Properties: []*parser.Property{
		{Name: "name", Type: "string"},
	}}
	root := &parser.Schema{
		OneOf: []*parser.Property{
			{RefName: "AIGatewayModelAPI"},
			{RefName: "AIGatewayModelModel"},
		},
		Discriminator: "type",
		DiscriminatorMapping: map[string]string{
			"api":   "AIGatewayModelAPI",
			"model": "AIGatewayModelModel",
		},
	}
	return &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"AIGatewayModel": root,
		},
		Schemas: map[string]*parser.Schema{
			"AIGatewayModelAPI":    modelAPI,
			"AIGatewayModelModel":  modelModel,
			"AIGatewayModelAccess": modelAccess,
			"AIGatewayAllowACL":    allowACL,
			"AIGatewayDenyACL":     denyACL,
		},
	}
}

func aiGatewayACLReferences(paths ...string) []config.ReferenceConfig {
	refs := make([]config.ReferenceConfig, 0, len(paths))
	for _, p := range paths {
		refs = append(refs, config.ReferenceConfig{
			Path:        p,
			Kinds:       []string{"AIGatewayConsumerGroup"},
			ResolvesTo:  "name",
			RefTypeName: "AIGatewayACLRef",
		})
	}
	return refs
}

// TestRefFieldTarget_RootUnionPath verifies that a reference path on a
// root-union entity descends through the synthesized <Entity>Config union
// (embedded as a pointer in the generated APISpec) into the selected variant.
func TestRefFieldTarget_RootUnionPath(t *testing.T) {
	parsed := modelRootUnionParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, nil)

	typeName, field, goPath, err := g.refFieldTarget("AIGatewayModel", config.ReferenceConfig{
		Path: "spec.apiSpec.api.access.acls.allow.allow",
	})
	require.NoError(t, err)
	require.Equal(t, "AIGatewayAllowACL", typeName)
	require.Equal(t, "allow", field)
	require.Equal(t, []GoPathSegment{
		{Name: "AIGatewayModelConfig", Pointer: true, UnionWrapper: true, UnionTypeName: "AIGatewayModelConfig"},
		{Name: "API", Pointer: true, JSONKey: "api", UnionVariant: true, VariantProperties: 2},
		{Name: "Access", Pointer: false, JSONKey: "access"},
		{Name: "Acls", Pointer: true, JSONKey: "acls", UnionWrapper: true, UnionTypeName: "AIGatewayModelAccessAcls"},
		{Name: "Allow", Pointer: true, JSONKey: "allow", UnionVariant: true, VariantProperties: 1},
		{Name: "Allow", Pointer: false, JSONKey: "allow"},
	}, goPath)

	// A segment not matching any root-union variant errors with the full path.
	_, _, _, err = g.refFieldTarget("AIGatewayModel", config.ReferenceConfig{ //nolint:dogsled // only the error matters here
		Path: "spec.apiSpec.nope.access.acls.allow.allow",
	})
	require.ErrorContains(t, err, "spec.apiSpec.nope.access.acls.allow.allow")
}

// TestGenerateSDKOps_ACLRefPayloadInjection verifies the client-needing builder
// generated for AIGatewayACLRef references: instead of flat leaf-key assignment
// it rebuilds the ACL union value in the SDK payload under its real key chain,
// switching on the CRD ACL union's selected variant, preserving sibling keys of
// the union's ancestors, and leaving the payload untouched when the union
// pointer is nil.
func TestGenerateSDKOps_ACLRefPayloadInjection(t *testing.T) {
	parsed := agentModelParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayAgent": aiGatewayACLReferences(
			"spec.apiSpec.access.acls.allow.allow",
			"spec.apiSpec.access.acls.deny.deny",
		),
	})
	schema := parsed.RequestBodies["AIGatewayAgent"]
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayAgentRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayAgentRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayAgent", schema, opsConfig)
	require.NoError(t, err)

	// Generated file stays parseable Go.
	_, err = format.Source([]byte(content))
	require.NoError(t, err)

	// Union guard: injection only runs when the CRD union pointer is set.
	require.Contains(t, content, "if obj.Spec.APISpec.Access.Acls != nil {")
	require.Contains(t, content, "acls := obj.Spec.APISpec.Access.Acls")
	// Sibling-preserving navigation to the union's parent map.
	require.Contains(t, content, `access, _ := payload["access"].(map[string]any)`)
	require.Contains(t, content, "if access == nil {")
	require.Contains(t, content, `payload["access"] = access`)
	// Variant switch follows the union discriminator and rebuilds the union
	// value with the resolved names only.
	require.Contains(t, content, "switch {")
	require.Contains(t, content, "case acls.Type == AIGatewayAgentAccessAclsTypeAllow:")
	require.Contains(t, content, "resolvedAccessAclsAllowAllow, err := resolveAIGatewayAgentAccessAclsAllowAllow(ctx, cl, obj)")
	require.Contains(t, content, `return nil, fmt.Errorf("resolving spec.apiSpec.access.acls.allow.allow references: %w", err)`)
	require.Contains(t, content, `access["acls"] = map[string]any{"allow": resolvedAccessAclsAllowAllow}`)
	require.Contains(t, content, "case acls.Type == AIGatewayAgentAccessAclsTypeDeny:")
	require.Contains(t, content, `access["acls"] = map[string]any{"deny": resolvedAccessAclsDenyDeny}`)
	// No flat top-level leaf-key assignment for ACL references inside SDK unions.
	require.NotContains(t, content, `payload["allow"]`)
	require.NotContains(t, content, `payload["deny"]`)
	// CRD and SDK paths agree: nothing to delete.
	require.NotContains(t, content, "delete(payload")
}

// TestGenerateSDKOps_RootUnionReferenceInjection verifies reference support in
// the root-union SDK-ops template: resolvers/accessors/ResolveKonnectReferences
// are emitted, the APISpec builders are split so an injected payload can be
// converted post-injection, and the client-needing entity wrapper injects the
// resolved names into the full payload (pre-selection: the root-union variant
// key "api" is part of the navigation chain and the existing selection step
// performs the SDK-side unwrap).
func TestGenerateSDKOps_RootUnionReferenceInjection(t *testing.T) {
	parsed := modelRootUnionParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"AIGatewayModel": aiGatewayACLReferences(
			"spec.apiSpec.api.access.acls.allow.allow",
			"spec.apiSpec.api.access.acls.deny.deny",
		),
	})
	schema := parsed.RequestBodies["AIGatewayModel"]
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayModelRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayModelRequest"},
		},
	}

	content, err := g.generateSDKOps("AIGatewayModel", schema, opsConfig)
	require.NoError(t, err)

	// Generated file stays parseable Go.
	_, err = format.Source([]byte(content))
	require.NoError(t, err)

	// Reference plumbing lands in the root-union template too.
	require.Contains(t, content, `"errors"`)
	require.Contains(t, content, "func RefsAtAIGatewayModelAPIAccessAclsAllowAllow(obj *AIGatewayModel) []AIGatewayACLRef {")
	require.Contains(t, content, "if obj.Spec.APISpec.AIGatewayModelConfig == nil {")
	require.Contains(t, content, "func resolveAIGatewayModelAPIAccessAclsAllowAllow(ctx context.Context, cl client.Client, obj *AIGatewayModel) ([]string, error)")
	require.Contains(t, content, "func (obj *AIGatewayModel) ResolveKonnectReferences(ctx context.Context, cl client.Client) error {")

	// APISpec builders are split so injection can happen between payload
	// computation and SDK request construction.
	require.Contains(t, content, "func (s *AIGatewayModelAPISpec) toCreateAIGatewayModelRequestFromPayload(payload map[string]any) (*sdkkonnectcomp.CreateAIGatewayModelRequest, error) {")
	require.Contains(t, content, "func (s *AIGatewayModelAPISpec) toUpdateAIGatewayModelRequestFromPayload(payload map[string]any) (*sdkkonnectcomp.UpdateAIGatewayModelRequest, error) {")

	// Client-needing entity wrapper injects into the payload and delegates.
	require.Contains(t, content, "func (obj *AIGatewayModel) ToCreateAIGatewayModelRequest(ctx context.Context, cl client.Client) (*sdkkonnectcomp.CreateAIGatewayModelRequest, error) {")
	require.Contains(t, content, "spec := &obj.Spec.APISpec")
	require.Contains(t, content, "if obj.Spec.APISpec.AIGatewayModelConfig != nil && obj.Spec.APISpec.AIGatewayModelConfig.API != nil && obj.Spec.APISpec.AIGatewayModelConfig.API.Access.Acls != nil {")
	require.Contains(t, content, "acls := obj.Spec.APISpec.AIGatewayModelConfig.API.Access.Acls")
	require.Contains(t, content, `api, _ := payload["api"].(map[string]any)`)
	require.Contains(t, content, `access, _ := api["access"].(map[string]any)`)
	require.Contains(t, content, "resolvedAPIAccessAclsAllowAllow, err := resolveAIGatewayModelAPIAccessAclsAllowAllow(ctx, cl, obj)")
	require.Contains(t, content, `access["acls"] = map[string]any{"allow": resolvedAPIAccessAclsAllowAllow}`)
	require.Contains(t, content, `access["acls"] = map[string]any{"deny": resolvedAPIAccessAclsDenyDeny}`)
	// Sibling-preserving write-back, innermost first.
	require.Contains(t, content, `api["access"] = access`)
	require.Contains(t, content, `payload["api"] = api`)
	require.Contains(t, content, "case acls.Type == AIGatewayModelAccessAclsTypeAllow:")
	require.Contains(t, content, "case acls.Type == AIGatewayModelAccessAclsTypeDeny:")
	require.Contains(t, content, "return spec.toCreateAIGatewayModelRequestFromPayload(payload)")
	require.Contains(t, content, "return spec.toUpdateAIGatewayModelRequestFromPayload(payload)")
	// The whole variant subtree is never rebuilt wholesale (that would drop
	// sibling fields like access.identity_providers).
	require.NotContains(t, content, `payload["api"] = map[string]any`)
	require.NotContains(t, content, `payload["access"]`)
}

// TestGenerateSDKOps_ACLRefInjectionUnsupportedShapes verifies that reference
// shapes the ACL payload injection cannot express fail generation loudly
// instead of silently emitting destructive payload rewrites.
func TestGenerateSDKOps_ACLRefInjectionUnsupportedShapes(t *testing.T) {
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayAgentRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayAgentRequest"},
		},
	}

	t.Run("union variant member with sibling fields", func(t *testing.T) {
		parsed := agentModelParsedSpec()
		// Give the allow variant a sibling field: rebuilding {"allow": [...]}
		// would silently drop it.
		parsed.Schemas["AIGatewayAllowACL"].Properties = append(
			parsed.Schemas["AIGatewayAllowACL"].Properties,
			&parser.Property{Name: "note", Type: "string"},
		)
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": aiGatewayACLReferences("spec.apiSpec.access.acls.allow.allow"),
		})
		_, err := g.generateSDKOps("AIGatewayAgent", parsed.RequestBodies["AIGatewayAgent"], opsConfig)
		require.Error(t, err)
		require.ErrorContains(t, err, "spec.apiSpec.access.acls.allow.allow")
		require.ErrorContains(t, err, "sibling fields")
	})

	t.Run("leaf directly below an inline root union variant", func(t *testing.T) {
		parsed := modelRootUnionParsedSpec()
		// The root-union variant is embedded inline in the SDK payload (no key
		// for the union itself), so its members cannot be rebuilt under a
		// union key.
		parsed.Schemas["AIGatewayModelAPI"].Properties = append(
			parsed.Schemas["AIGatewayModelAPI"].Properties,
			&parser.Property{Name: "policies", Type: "array", Items: &parser.Property{Type: "string"}},
		)
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayModel": aiGatewayACLReferences("spec.apiSpec.api.policies"),
		})
		modelOpsConfig := &config.EntityOpsConfig{
			Ops: map[string]*config.OpConfig{
				"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayModelRequest"},
				"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayModelRequest"},
			},
		}
		_, err := g.generateSDKOps("AIGatewayModel", parsed.RequestBodies["AIGatewayModel"], modelOpsConfig)
		require.Error(t, err)
		require.ErrorContains(t, err, "spec.apiSpec.api.policies")
	})

	t.Run("leaf below an intermediate object under the union variant", func(t *testing.T) {
		parsed := agentModelParsedSpec()
		// allow -> rules (object) -> allow (array): the leaf is not directly
		// on the union variant member.
		parsed.Schemas["AIGatewayAllowACL"].Properties = []*parser.Property{
			{Name: "rules", Type: "object", Properties: []*parser.Property{
				{Name: "allow", Type: "array", Items: &parser.Property{Type: "string"}},
			}},
		}
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": aiGatewayACLReferences("spec.apiSpec.access.acls.allow.rules.allow"),
		})
		_, err := g.generateSDKOps("AIGatewayAgent", parsed.RequestBodies["AIGatewayAgent"], opsConfig)
		require.Error(t, err)
		require.ErrorContains(t, err, "spec.apiSpec.access.acls.allow.rules.allow")
	})

	t.Run("nested union ref without AIGatewayACLRef type is rejected", func(t *testing.T) {
		parsed := agentModelParsedSpec()
		ref := config.ReferenceConfig{
			Path:        "spec.apiSpec.access.acls.allow.allow",
			Kinds:       []string{"AIGatewayConsumerGroup"},
			ResolvesTo:  "name",
			RefTypeName: "OtherACLRef",
		}
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {ref},
		})

		t.Run("nested non-ACL ref is rejected", func(t *testing.T) {
			parsed := agentModelParsedSpec()
			ref := config.ReferenceConfig{
				Path:       "spec.apiSpec.access.throttle.limits",
				Kinds:      []string{"AIGatewayThrottleLimit"},
				ResolvesTo: "id",
			}
			g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
				"AIGatewayAgent": {ref},
			})
			err := g.validateReferences(parsed)
			require.Error(t, err)
			require.ErrorContains(t, err, "spec.apiSpec.access.throttle.limits")
			require.ErrorContains(t, err, "nested references must use refTypeName AIGatewayACLRef")
		})
		_, err := g.generateSDKOps("AIGatewayAgent", parsed.RequestBodies["AIGatewayAgent"], opsConfig)
		require.Error(t, err)
		require.ErrorContains(t, err, "spec.apiSpec.access.acls.allow.allow")
		require.ErrorContains(t, err, "SDK-union references must use refTypeName AIGatewayACLRef")
	})

	t.Run("AIGatewayACLRef with unsupported suffix is rejected", func(t *testing.T) {
		parsed := agentModelParsedSpec()
		ref := config.ReferenceConfig{
			Path:        "spec.apiSpec.access.throttle.limits",
			Kinds:       []string{"AIGatewayConsumerGroup"},
			ResolvesTo:  "name",
			RefTypeName: "AIGatewayACLRef",
		}
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
			"AIGatewayAgent": {ref},
		})
		_, err := g.generateSDKOps("AIGatewayAgent", parsed.RequestBodies["AIGatewayAgent"], opsConfig)
		require.Error(t, err)
		require.ErrorContains(t, err, "spec.apiSpec.access.throttle.limits")
		require.ErrorContains(t, err, "only supports paths ending in access.acls.allow.allow or access.acls.deny.deny")
	})
}
