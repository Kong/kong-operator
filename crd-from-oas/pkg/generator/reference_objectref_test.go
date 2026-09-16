package generator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// portalPageParsedSpec builds a fixture whose PortalPage schema mirrors the
// real one: a plain string field, a plain object field, plus an OAS reference
// property (parent_page_id, string uuid, IsReference) that the CRD renders as
// a single *commonv1alpha1.ObjectRef field named ParentPageIDRef.
func portalPageParsedSpec() *parser.ParsedSpec {
	return &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"PortalPage": {
				Properties: []*parser.Property{
					{Name: "title", Type: "string"},
					{Name: "settings", Type: "object"},
					{Name: "parent_page_id", Type: "string", Format: "uuid", IsReference: true},
				},
				// PortalPage is a child of Portal; same-type references must
				// stay within the same parent.
				Dependencies: []*parser.Dependency{{
					EntityName:         "Portal",
					AccessorEntityName: "Portal",
					FieldName:          "PortalRef",
					JSONName:           "portal_ref",
				}},
			},
		},
		Schemas: map[string]*parser.Schema{},
	}
}

func portalPageRefConfig() config.ReferenceConfig {
	return config.ReferenceConfig{
		Path:       "spec.apiSpec.parentPageIDRef",
		Kinds:      []string{"PortalPage"},
		ResolvesTo: "id",
	}
}

// TestRefFieldTarget_ObjectRefField verifies that a reference path naming an
// OAS reference property (via its Ref-suffixed CRD JSON name) resolves to a
// single ObjectRefField leaf segment instead of erroring on the non-array
// scalar property.
func TestRefFieldTarget_ObjectRefField(t *testing.T) {
	parsed := portalPageParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, nil)

	typeName, field, goPath, err := g.refFieldTarget("PortalPage", portalPageRefConfig())
	require.NoError(t, err)
	require.Equal(t, "PortalPageAPISpec", typeName)
	require.Equal(t, "parentPageID", field)
	require.Equal(t, []GoPathSegment{
		{Name: "ParentPageIDRef", Pointer: true, JSONKey: "parentPageIDRef", ObjectRefField: true},
	}, goPath)

	// The plain OAS property name (without the Ref suffix) matches too.
	_, _, goPath, err = g.refFieldTarget("PortalPage", config.ReferenceConfig{
		Path: "spec.apiSpec.parentPageID",
	})
	require.NoError(t, err)
	require.Equal(t, []GoPathSegment{
		{Name: "ParentPageIDRef", Pointer: true, JSONKey: "parentPageIDRef", ObjectRefField: true},
	}, goPath)

	// A plain (non-reference) string property directly on apiSpec is accepted
	// too: it resolves as a direct scalar leaf (e.g. AIGatewaySNI's
	// "certificate" field), without the ObjectRefField flag.
	_, _, goPath, err = g.refFieldTarget("PortalPage", config.ReferenceConfig{
		Path: "spec.apiSpec.title",
	})
	require.NoError(t, err)
	require.Equal(t, []GoPathSegment{
		{Name: "Title", Pointer: true, JSONKey: "title"},
	}, goPath)

	// A direct object-typed property is still rejected: it has no reference
	// cardinality.
	_, _, _, err = g.refFieldTarget("PortalPage", config.ReferenceConfig{ //nolint:dogsled // only the error matters here
		Path: "spec.apiSpec.settings",
	})
	require.ErrorContains(t, err, "spec.apiSpec.settings")
}

// TestTemplateReferences_ObjectRefField verifies the template-facing config
// derived for an ObjectRefField reference: single (non-nested) reference, the
// Ref-suffixed CRD field names, and the OAS property's snake_case SDK payload
// key (parent_page_id, not parent_page_id_ref).
func TestTemplateReferences_ObjectRefField(t *testing.T) {
	parsed := portalPageParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {portalPageRefConfig()},
	})

	refs := g.templateReferences("PortalPage")
	require.Len(t, refs, 1)
	ref := refs[0]
	require.True(t, ref.ObjectRefField)
	require.False(t, ref.NestedRef)
	require.Equal(t, "ParentPageIDRef", ref.GoFieldName)
	require.Equal(t, "parentPageIDRef", ref.JSONFieldName)
	require.Equal(t, "parent_page_id", ref.SDKJSONFieldName)
	require.Equal(t, "obj.Spec.APISpec.ParentPageIDRef", ref.RefsExpr)
	require.Equal(t, "PortalPage", ref.DefaultKind)
	require.False(t, ref.MultiKind)
	// Same-type ObjectRefField references carry the entity's parent
	// reference field so the resolver can reject cross-parent references.
	require.Equal(t, "PortalRef", ref.SameParentRefField)
	require.Equal(t, "Portal", ref.SameParentRefKind)
}

// TestTemplateReferences_ObjectRefField_NoParentNoGuard verifies that a
// same-type ObjectRefField reference on a root entity (no parent dependency)
// does not emit a same-parent guard.
func TestTemplateReferences_ObjectRefField_NoParentNoGuard(t *testing.T) {
	parsed := portalPageParsedSpec()
	parsed.RequestBodies["PortalPage"].Dependencies = nil
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {portalPageRefConfig()},
	})

	refs := g.templateReferences("PortalPage")
	require.Len(t, refs, 1)
	require.True(t, refs[0].ObjectRefField)
	require.Empty(t, refs[0].SameParentRefField)
	require.Empty(t, refs[0].SameParentRefKind)
}

// TestValidateReferences_ObjectRefField verifies the additional validation
// rules for ObjectRefField references: nested paths and multi-kind references
// are rejected with clear errors.
func TestValidateReferences_ObjectRefField(t *testing.T) {
	parsed := portalPageParsedSpec()

	// Multi-kind ObjectRefField references are not supported: ObjectRef has
	// no kind field to dispatch on.
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {
			{
				Path:       "spec.apiSpec.parentPageIDRef",
				Kinds:      []string{"PortalPage", "Portal"},
				ResolvesTo: "id",
			},
		},
	})
	require.ErrorContains(t, g.validateReferences(parsed), "exactly one kind")

	// Nested ObjectRefField paths are not supported.
	parsedNested := portalPageParsedSpec()
	parsedNested.Schemas["PortalPageWrapper"] = &parser.Schema{Properties: []*parser.Property{
		{Name: "parent_page_id", Type: "string", Format: "uuid", IsReference: true},
	}}
	parsedNested.RequestBodies["PortalPage"].Properties = append(
		parsedNested.RequestBodies["PortalPage"].Properties,
		&parser.Property{Name: "wrapper", Type: "object", RefName: "PortalPageWrapper"},
	)
	g = newTestGeneratorWithParsed(t, parsedNested, map[string][]config.ReferenceConfig{
		"PortalPage": {
			{
				Path:       "spec.apiSpec.wrapper.parentPageIDRef",
				Kinds:      []string{"PortalPage"},
				ResolvesTo: "id",
			},
		},
	})
	require.ErrorContains(t, g.validateReferences(parsedNested), "top level")

	// The valid single-kind top-level configuration passes.
	g = newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {portalPageRefConfig()},
	})
	require.NoError(t, g.validateReferences(parsed))
}

// TestGenerateSDKOps_ObjectRefFieldResolver verifies the resolver generated
// for an ObjectRefField reference: it reads the single *ObjectRef field,
// passes konnectID through, looks namespacedRef up via the client, and
// injects the resolved ID under the OAS property's snake_case payload key.
func TestGenerateSDKOps_ObjectRefFieldResolver(t *testing.T) {
	parsed := portalPageParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {portalPageRefConfig()},
	})
	// Mirror the real config: ObjectRef is imported from the common API
	// package, so generated code qualifies it with the commonv1alpha1 alias.
	g.config.CommonTypes = &config.CommonTypesConfig{
		ObjectRef: &config.ObjectRefConfig{
			Import: &config.ImportConfig{
				Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
				Alias: "commonv1alpha1",
			},
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.PortalPageCreate"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.PortalPageUpdate"},
		},
	}

	content, err := g.generateSDKOps("PortalPage", parsed.RequestBodies["PortalPage"], opsConfig)
	require.NoError(t, err)

	// The ObjectRef type is referenced through the commonv1alpha1 import.
	require.Contains(t, content, `commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"`)
	// Resolver reads the single ref field and nil-guards it.
	require.Contains(t, content, "func resolvePortalPageParentPageIDRef(ctx context.Context, cl client.Client, obj *PortalPage) ([]string, error)")
	require.Contains(t, content, "ref := obj.Spec.APISpec.ParentPageIDRef")
	require.Contains(t, content, "if ref == nil {")
	// konnectID passes through without a lookup.
	require.Contains(t, content, "case commonv1alpha1.ObjectRefTypeKonnectID:")
	require.Contains(t, content, "return []string{*ref.KonnectID}, nil")
	// namespacedRef resolves via the client, defaulting the namespace and
	// rejecting cross-namespace references.
	require.Contains(t, content, "case commonv1alpha1.ObjectRefTypeNamespacedRef:")
	require.Contains(t, content, "ns := obj.GetNamespace()")
	require.Contains(t, content, "ReferenceCrossNamespaceError")
	require.Contains(t, content, `Kind: "PortalPage"`)
	require.Contains(t, content, "id := referenced.GetKonnectID()")
	require.Contains(t, content, "return []string{id}, nil")
	// Same-type references are guarded against pointing at an object under a
	// different parent.
	require.Contains(t, content, "commonv1alpha1.ObjectRefsDiffer(obj.Spec.PortalRef, referenced.Spec.PortalRef, obj.GetNamespace(), ns)")
	require.Contains(t, content, `ReferenceDifferentParentError{Kind: "PortalPage", Namespace: ns, Name: name, ParentKind: "Portal"}`)
	// The resolved value is injected under the OAS property's payload key.
	require.Contains(t, content, `payload["parent_page_id"] = resolvedParentPageIDRef[0]`)
	require.NotContains(t, content, `payload["parent_page_id_ref"]`)
	// No ref-struct accessor is emitted for ObjectRefField references.
	require.NotContains(t, content, "RefsAtPortalPage")
}

// TestGenerateReferencesFile_ObjectRefFieldSkipped verifies that an
// ObjectRefField reference does not produce a ref struct in the references
// file (the CRD field stays *commonv1alpha1.ObjectRef), while the file itself
// is still emitted with the shared aliases and constants.
func TestGenerateReferencesFile_ObjectRefFieldSkipped(t *testing.T) {
	parsed := portalPageParsedSpec()
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {portalPageRefConfig()},
	})

	content, err := g.GenerateReferencesFile()
	require.NoError(t, err)
	require.Contains(t, content, "ReferenceNotFoundError")
	require.NotContains(t, content, "type PortalPageRef struct")
}

// TestGenerateReferencesFile_MixedShapeRefTypeGenerated verifies that a ref
// struct is still generated when its TypeName is shared between an
// ObjectRefField reference and an array-shaped reference: config validation
// does not distinguish shapes, so the array-shaped one still needs the
// struct.
func TestGenerateReferencesFile_MixedShapeRefTypeGenerated(t *testing.T) {
	parsed := portalPageParsedSpec()
	parsed.RequestBodies["PortalPage"].Properties = append(
		parsed.RequestBodies["PortalPage"].Properties,
		&parser.Property{
			Name:  "related_page_ids",
			Type:  "array",
			Items: &parser.Property{Type: "string", Format: "uuid", IsReference: true},
		},
	)
	g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{
		"PortalPage": {
			portalPageRefConfig(),
			{
				Path:       "spec.apiSpec.relatedPageIDs",
				Kinds:      []string{"PortalPage"},
				ResolvesTo: "id",
			},
		},
	})

	content, err := g.GenerateReferencesFile()
	require.NoError(t, err)
	require.Contains(t, content, "type PortalPageRef struct")
}
