package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

// TestGenerate_GatewayRef covers entities configured with reconciler.gatewayRef:
// they keep their OpenAPI-derived parent ref field, deprecated and optional,
// and get a GatewayRef typed field which can also reference non-Konnect
// gateways.
func TestGenerate_GatewayRef(t *testing.T) {
	const entity = "EventGatewayListener"

	newGenerator := func(gatewayRef *config.GatewayRefConfig) *Generator {
		return NewGenerator(Config{
			APIGroup:             "configuration.konghq.com",
			APIVersion:           "v1alpha1",
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/configuration/v1alpha1",
			APIGroupPackageAlias: "configurationv1alpha1",
			CommonTypes: &config.CommonTypesConfig{
				ObjectRef: &config.ObjectRefConfig{
					Import: &config.ImportConfig{
						Path:  "github.com/kong/kong-operator/v2/api/common/v1alpha1",
						Alias: "commonv1alpha1",
					},
				},
			},
			ReconcilerConfig: map[string]*config.ReconcilerConfig{
				entity: {
					IsRoot:          new(false),
					ParentEntityGVK: &config.EntityGVKConfig{Group: "konnect.konghq.com", Kind: "KonnectEventGateway"},
					GatewayRef:      gatewayRef,
				},
			},
		})
	}
	schema := func() *parser.Schema {
		return &parser.Schema{
			Name: "Create" + entity,
			Dependencies: []*parser.Dependency{
				{ParamName: "gatewayId", EntityName: "Gateway", AccessorEntityName: "EventGateway", FieldName: "GatewayRef", JSONName: "gatewayRef"},
			},
		}
	}
	gatewayRef := &config.GatewayRefConfig{
		FieldName: "parentRef",
		AdditionalKinds: []config.EntityGVKConfig{
			{Group: "eventgateway.konghq.com", Version: "v1alpha1", Kind: "OnPremEventGateway"},
		},
	}

	t.Run("types", func(t *testing.T) {
		content, err := newGenerator(gatewayRef).generateCRDType("Create"+entity, schema())
		require.NoError(t, err)

		// The legacy field stays, optional and deprecated.
		// It is not marked with a "Deprecated:" paragraph: the field must
		// keep being handled, and that would flag every use of it.
		assert.Contains(t, content, "// This field is deprecated, use parentRef instead.")
		assert.NotContains(t, content, "// Deprecated:")
		assert.Contains(t, content, "\t// +optional\n\tGatewayRef commonv1alpha1.ObjectRef `json:\"gatewayRef,omitzero\"`")
		// The new field only accepts the configured group and kind pairs.
		assert.Contains(t, content, "ParentRef *commonv1alpha1.GatewayRef `json:\"parentRef,omitempty\"`")
		assert.Contains(t, content, `rule="(self.group == 'konnect.konghq.com' && self.kind == 'KonnectEventGateway') || (self.group == 'eventgateway.konghq.com' && self.kind == 'OnPremEventGateway')"`)

		assert.Contains(t, content, `rule="has(self.spec.gatewayRef) != has(self.spec.parentRef)", message="exactly one of spec.gatewayRef and spec.parentRef must be set"`)
		assert.Contains(t, content, `rule="(has(self.spec.parentRef) ? self.spec.parentRef.kind : 'KonnectEventGateway') == (has(oldSelf.spec.parentRef) ? oldSelf.spec.parentRef.kind : 'KonnectEventGateway')"`)
		assert.Contains(t, content, `message="spec.gatewayRef is immutable when an entity is already Programmed"`)
		assert.Contains(t, content, `message="spec.parentRef is immutable when an entity is already Programmed"`)
		assert.Contains(t, content, `rule="!has(oldSelf.spec.parentRef) || has(self.spec.parentRef)", message="spec.parentRef cannot be replaced by the deprecated spec.gatewayRef"`)
		assert.Contains(t, content, `message="spec.gatewayRef can only be replaced by a spec.parentRef referencing the same gateway once an entity is already Programmed"`)
		// "namespace" is a reserved word in CEL and must be escaped.
		assert.Contains(t, content, "self.spec.parentRef.__namespace__ == oldSelf.spec.gatewayRef.namespacedRef.__namespace__")
		assert.NotContains(t, content, "parentRef.namespace")
	})

	t.Run("funcs", func(t *testing.T) {
		content, err := newGenerator(gatewayRef).generateCRDFuncs("Create"+entity, schema())
		require.NoError(t, err)

		assert.Contains(t, content, "if ref := obj.Spec.ParentRef; ref != nil {\n\t\treturn ref.ToObjectRef()\n\t}\n\treturn obj.Spec.GatewayRef")
		assert.Contains(t, content, "obj.Spec.ParentRef = nil\n\tobj.Spec.GatewayRef = ref")
		assert.Contains(t, content, "func (obj *EventGatewayListener) SkipKonnectReconciliation() bool {")
		assert.Contains(t, content, `!ref.IsGroupKind("konnect.konghq.com", "KonnectEventGateway")`)
		assert.Contains(t, content, `ref.IsGroupKind("eventgateway.konghq.com", "OnPremEventGateway") {`)
	})

	t.Run("watch and index", func(t *testing.T) {
		g := newGenerator(gatewayRef)
		rc := g.config.ReconcilerConfig[entity]
		metadata, err := g.reconcilerEntityMetadata(entity, schema(), rc)
		require.NoError(t, err)

		watch, err := g.generateWatch(metadata, rc)
		require.NoError(t, err)
		assert.Contains(t, watch, "return !ent.SkipKonnectReconciliation()")

		index, err := g.generateIndex(metadata, rc)
		require.NoError(t, err)
		assert.Contains(t, index, `IndexFieldEventGatewayListenerOnKonnectEventGatewayRef = "eventGatewayListenerOnKonnectEventGatewayRef"`)
		assert.Contains(t, index, `IndexFieldEventGatewayListenerOnOnPremEventGatewayRef = "eventGatewayListenerOnOnPremEventGatewayRef"`)
		assert.Contains(t, index, "if ent.SkipKonnectReconciliation() {")
		assert.Contains(t, index, `!ref.IsGroupKind("eventgateway.konghq.com", "OnPremEventGateway")`)
	})

	t.Run("without additional kinds", func(t *testing.T) {
		g := newGenerator(&config.GatewayRefConfig{FieldName: "parentRef"})
		rc := g.config.ReconcilerConfig[entity]

		types, err := g.generateCRDType("Create"+entity, schema())
		require.NoError(t, err)
		assert.Contains(t, types, `rule="(self.group == 'konnect.konghq.com' && self.kind == 'KonnectEventGateway')", message="parentRef must reference one of: konnect.konghq.com/KonnectEventGateway"`)
		assert.Contains(t, types, `message="exactly one of spec.gatewayRef and spec.parentRef must be set"`)
		assert.Contains(t, types, `message="spec.parentRef cannot be replaced by the deprecated spec.gatewayRef"`)
		// With a single kind there is no kind to switch to.
		assert.NotContains(t, types, "the kind of the referenced gateway is immutable")

		// Every entity is reconciled against Konnect: nothing to skip.
		funcs, err := g.generateCRDFuncs("Create"+entity, schema())
		require.NoError(t, err)
		assert.Contains(t, funcs, "if ref := obj.Spec.ParentRef; ref != nil {\n\t\treturn ref.ToObjectRef()")
		assert.NotContains(t, funcs, "SkipKonnectReconciliation")

		metadata, err := g.reconcilerEntityMetadata(entity, schema(), rc)
		require.NoError(t, err)
		watch, err := g.generateWatch(metadata, rc)
		require.NoError(t, err)
		assert.NotContains(t, watch, "SkipKonnectReconciliation")
		assert.Contains(t, watch, "return b.For(&configurationv1alpha1.EventGatewayListener{})")

		index, err := g.generateIndex(metadata, rc)
		require.NoError(t, err)
		assert.Contains(t, index, "ref := ent.GetParentRef()")
		assert.NotContains(t, index, "SkipKonnectReconciliation")
		assert.NotContains(t, index, "OnPrem")
	})

	t.Run("entities without gatewayRef are unchanged", func(t *testing.T) {
		g := newGenerator(nil)
		types, err := g.generateCRDType("Create"+entity, schema())
		require.NoError(t, err)
		assert.Contains(t, types, "\t// +required\n\tGatewayRef commonv1alpha1.ObjectRef `json:\"gatewayRef,omitzero\"`")
		assert.NotContains(t, types, "parentRef")

		funcs, err := g.generateCRDFuncs("Create"+entity, schema())
		require.NoError(t, err)
		assert.NotContains(t, funcs, "SkipKonnectReconciliation")
		assert.NotContains(t, funcs, "ParentRef;")
	})
}
