package generator

import (
	"go/format"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
)

func aiGatewayCustomPolicyWatchMetadata() reconcilerEntityMetadata {
	return reconcilerEntityMetadata{
		EntityName:                 "AIGatewayCustomPolicy",
		EntityNameLowerCamel:       "aiGatewayCustomPolicy",
		ParentEntityName:           "KonnectAIGateway",
		ParentRefFieldName:         "AIGatewayRef",
		APIGroupPackagePath:        "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",
		APIGroupPackageAlias:       "aiconfigurationv1alpha1",
		ParentAPIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		ParentAPIGroupPackageAlias: "konnectv1alpha1",
	}
}

func TestGenerateWatch_ReverseWatch(t *testing.T) {
	newGenerator := func(refs map[string][]config.ReferenceConfig) *Generator {
		return NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",
			APIGroupPackageAlias: "aiconfigurationv1alpha1",
			References:           refs,
		})
	}
	rc := &config.ReconcilerConfig{IsRoot: new(false), ParentEntityType: "KonnectAIGateway"}

	t.Run("referenced kind watches its reverseWatch referrers", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		g := newGenerator(map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}})

		content, err := g.generateWatch(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)

		assert.Contains(t, content, "&aiconfigurationv1alpha1.AIGatewayPolicy{},")
		assert.Contains(t, content, "enqueueAIGatewayCustomPolicyForAIGatewayPolicy(cl),")
		assert.Contains(t, content, "func enqueueAIGatewayCustomPolicyForAIGatewayPolicy(")
		// References are enqueued directly from their keys, without listing.
		assert.Contains(t, content, "for _, key := range aiconfigurationv1alpha1.AIGatewayPolicyRefsToAIGatewayCustomPolicy(referrer) {")
		// The literal injectInto target is looked up through the Konnect-key index.
		assert.Contains(t, content, "value, gatewayID := referrer.Spec.APISpec.Type, referrer.GetGatewayID()")
		assert.Contains(t, content, `index.IndexFieldAIGatewayCustomPolicyOnKonnectName: gatewayID + "/" + value,`)
		assert.NotContains(t, content, "cl.List(ctx, &l); err")
	})

	t.Run("reverseWatch without injectInto needs no index lookup", func(t *testing.T) {
		ref := config.ReferenceConfig{
			Path: "spec.apiSpec.policies", Kinds: []string{"AIGatewayCustomPolicy"}, ResolvesTo: "name", ReverseWatch: true,
		}
		g := newGenerator(map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}})

		content, err := g.generateWatch(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)
		assert.Contains(t, content, "for _, key := range aiconfigurationv1alpha1.AIGatewayPolicyRefsToAIGatewayCustomPolicy(referrer) {")
		assert.NotContains(t, content, "IndexFieldAIGatewayCustomPolicyOnKonnectName")
	})

	t.Run("references without reverseWatch add no watch", func(t *testing.T) {
		g := newGenerator(map[string][]config.ReferenceConfig{"AIGatewayPolicy": {customPolicyRefConfig()}})

		content, err := g.generateWatch(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		assert.NotContains(t, content, "AIGatewayPolicy{}")
		assert.NotContains(t, content, "enqueueAIGatewayCustomPolicyForAIGatewayPolicy")
	})

	t.Run("reverse watch colliding with a forward reference is rejected", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		g := newGenerator(map[string][]config.ReferenceConfig{
			"AIGatewayPolicy": {ref},
			"AIGatewayCustomPolicy": {{
				Path: "spec.apiSpec.policies", Kinds: []string{"AIGatewayPolicy"}, ResolvesTo: "name",
			}},
		})

		_, err := g.generateWatch(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.ErrorContains(t, err, "collides")
	})
}

func TestGenerateSDKOps_ReverseWatchRefsTo(t *testing.T) {
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayPolicyRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayPolicyRequest"},
		},
	}

	t.Run("reverseWatch reference gets a RefsTo accessor", func(t *testing.T) {
		parsed := policyParsedSpec()
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}})
		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))

		content, err := g.generateSDKOps("AIGatewayPolicy", parsed.RequestBodies["AIGatewayPolicy"], opsConfig)
		require.NoError(t, err)

		require.Contains(t, content, "func AIGatewayPolicyRefsToAIGatewayCustomPolicy(obj *AIGatewayPolicy) []client.ObjectKey {")
		require.Contains(t, content, "for _, ref := range RefsAtAIGatewayPolicyCustomPolicyRef(obj) {")
		require.Contains(t, content, "keys = append(keys, client.ObjectKey{Namespace: ns, Name: ref.Name})")
	})

	t.Run("no accessor without reverseWatch", func(t *testing.T) {
		parsed := policyParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{"AIGatewayPolicy": {customPolicyRefConfig()}})
		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))

		content, err := g.generateSDKOps("AIGatewayPolicy", parsed.RequestBodies["AIGatewayPolicy"], opsConfig)
		require.NoError(t, err)
		require.NotContains(t, content, "AIGatewayPolicyRefsToAIGatewayCustomPolicy")
	})
}

func TestValidateReverseWatchReferences(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ref     TemplateReferenceConfig
		wantErr bool
	}{
		{name: "direct reference", ref: TemplateReferenceConfig{ReferenceConfig: config.ReferenceConfig{Path: "spec.apiSpec.x", ReverseWatch: true}}},
		{name: "nested list-of-lists", ref: TemplateReferenceConfig{ReferenceConfig: config.ReferenceConfig{Path: "spec.apiSpec.x.y", ReverseWatch: true}, NestedArrayList: true}, wantErr: true},
		{name: "ObjectRef field", ref: TemplateReferenceConfig{ReferenceConfig: config.ReferenceConfig{Path: "spec.apiSpec.xRef", ReverseWatch: true}, ObjectRefField: true}, wantErr: true},
		{name: "unsupported shape without reverseWatch", ref: TemplateReferenceConfig{ReferenceConfig: config.ReferenceConfig{Path: "spec.apiSpec.xRef"}, ObjectRefField: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReverseWatchReferences("Entity", []TemplateReferenceConfig{tc.ref})
			if tc.wantErr {
				require.ErrorContains(t, err, "reverseWatch is not supported")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGenerateIndex_ReverseWatchInjectIntoIndexes(t *testing.T) {
	rc := &config.ReconcilerConfig{IsRoot: new(false), ParentEntityType: "KonnectAIGateway"}
	policyMetadata := aiGatewayCustomPolicyWatchMetadata()
	policyMetadata.EntityName = "AIGatewayPolicy"
	policyMetadata.EntityNameLowerCamel = "aiGatewayPolicy"
	newGenerator := func(ref config.ReferenceConfig) *Generator {
		return NewGenerator(Config{
			APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",
			APIGroupPackageAlias: "aiconfigurationv1alpha1",
			References:           map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}},
		})
	}

	t.Run("referenced kind is indexed on its Konnect key", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		content, err := newGenerator(ref).generateIndex(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)

		assert.Contains(t, content, `IndexFieldAIGatewayCustomPolicyOnKonnectName = "aiGatewayCustomPolicyOnKonnectName"`)
		assert.Contains(t, content, "ExtractValueFn: aiGatewayCustomPolicyOnKonnectName,")
		assert.Contains(t, content, "gatewayID, value := ent.GetGatewayID(), ent.GetKonnectName()")
		assert.Contains(t, content, `return []string{gatewayID + "/" + value}`)
	})

	t.Run("referrer is indexed on its injectInto target", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		content, err := newGenerator(ref).generateIndex(policyMetadata, rc)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)

		assert.Contains(t, content, `IndexFieldAIGatewayPolicyOnType = "aiGatewayPolicyOnType"`)
		assert.Contains(t, content, "ExtractValueFn: aiGatewayPolicyOnType,")
		assert.Contains(t, content, "gatewayID, value := ent.GetGatewayID(), ent.Spec.APISpec.Type")
	})

	t.Run("references resolving to an ID index the Konnect ID", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ReverseWatch = true
		ref.ResolvesTo = "id"
		content, err := newGenerator(ref).generateIndex(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		assert.Contains(t, content, "IndexFieldAIGatewayCustomPolicyOnKonnectID")
		assert.Contains(t, content, "ent.GetKonnectID()")
	})

	t.Run("without reverseWatch only the referrer's injectInto target is indexed", func(t *testing.T) {
		// The referrer's own watch on the referenced kind uses the target
		// index; only the reverse watch needs the Konnect-key index.
		content, err := newGenerator(customPolicyRefConfig()).generateIndex(aiGatewayCustomPolicyWatchMetadata(), rc)
		require.NoError(t, err)
		assert.NotContains(t, content, "OnKonnectName")

		content, err = newGenerator(customPolicyRefConfig()).generateIndex(policyMetadata, rc)
		require.NoError(t, err)
		assert.Contains(t, content, `IndexFieldAIGatewayPolicyOnType = "aiGatewayPolicyOnType"`)
	})

	t.Run("no target index without injectInto", func(t *testing.T) {
		ref := config.ReferenceConfig{Path: "spec.apiSpec.policies", Kinds: []string{"AIGatewayCustomPolicy"}, ResolvesTo: "name"}
		content, err := newGenerator(ref).generateIndex(policyMetadata, rc)
		require.NoError(t, err)
		assert.NotContains(t, content, "IndexFieldAIGatewayPolicyOnType")
	})
}

func TestGenerateWatch_InjectIntoForwardWatchFindsLiteralTargets(t *testing.T) {
	policyMetadata := aiGatewayCustomPolicyWatchMetadata()
	policyMetadata.EntityName = "AIGatewayPolicy"
	policyMetadata.EntityNameLowerCamel = "aiGatewayPolicy"
	rc := &config.ReconcilerConfig{IsRoot: new(false), ParentEntityType: "KonnectAIGateway"}
	newGenerator := func(ref config.ReferenceConfig) *Generator {
		parsed := policyParsedSpec()
		g := newTestGeneratorWithParsed(t, parsed, map[string][]config.ReferenceConfig{"AIGatewayPolicy": {ref}})
		g.config.APIGroupPackagePath = "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
		g.config.APIGroupPackageAlias = "aiconfigurationv1alpha1"
		require.NoError(t, g.addInjectIntoReferenceProperties(parsed))
		return g
	}

	t.Run("referrers setting the target literally are enqueued through the target index", func(t *testing.T) {
		content, err := newGenerator(customPolicyRefConfig()).generateWatch(policyMetadata, rc)
		require.NoError(t, err)
		_, err = format.Source([]byte(content))
		require.NoError(t, err)

		assert.Contains(t, content, "index.IndexFieldAIGatewayPolicyOnAIGatewayCustomPolicyRef: client.ObjectKeyFromObject(ref).String(),")
		assert.Contains(t, content, "gatewayID, value := ref.GetGatewayID(), ref.GetKonnectName(); gatewayID != \"\" && value != \"\"")
		assert.Contains(t, content, `index.IndexFieldAIGatewayPolicyOnType: gatewayID + "/" + value,`)
	})

	t.Run("references resolving to an ID look up the Konnect ID", func(t *testing.T) {
		ref := customPolicyRefConfig()
		ref.ResolvesTo = "id"
		content, err := newGenerator(ref).generateWatch(policyMetadata, rc)
		require.NoError(t, err)
		assert.Contains(t, content, "ref.GetKonnectID()")
	})

	t.Run("references without injectInto need no literal lookup", func(t *testing.T) {
		ref := config.ReferenceConfig{Path: "spec.apiSpec.policies", Kinds: []string{"AIGatewayCustomPolicy"}, ResolvesTo: "name"}
		content, err := newGenerator(ref).generateWatch(policyMetadata, rc)
		require.NoError(t, err)
		assert.NotContains(t, content, "IndexFieldAIGatewayPolicyOnType")
	})
}
