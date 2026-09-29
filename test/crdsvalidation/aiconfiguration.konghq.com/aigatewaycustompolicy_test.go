package crdsvalidation

import (
	"strings"
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func inlineLua(src string) aiconfigurationv1alpha1.ConfigMapDataSource {
	return aiconfigurationv1alpha1.ConfigMapDataSource{
		Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
		Value: new(src),
	}
}

func luaFromConfigMap(key string) aiconfigurationv1alpha1.ConfigMapDataSource {
	return aiconfigurationv1alpha1.ConfigMapDataSource{
		Type: aiconfigurationv1alpha1.ConfigMapDataSourceTypeConfigMapRef,
		ConfigMapRef: &aiconfigurationv1alpha1.ConfigMapDataSourceRef{
			Name: "custom-policy-lua",
			Key:  key,
		},
	}
}

func validAIGatewayCustomPolicyInstalled(ns string) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
	return &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-ai-gw-cp",
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled,
					Installed: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyInstalledRequest{
						Name:        "my-installed-policy",
						DisplayName: "My installed policy",
						Schema:      inlineLua("return {}"),
					},
				},
			},
		},
	}
}

func validAIGatewayCustomPolicyStreaming(ns string) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
	obj := validAIGatewayCustomPolicyInstalled(ns)
	obj.Spec.APISpec.AIGatewayCustomPolicyConfig = &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
		Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
		Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
			Name:        "my-streaming-policy",
			DisplayName: "My streaming policy",
			Schema:      inlineLua("return {}"),
			Handler:     inlineLua("return {}"),
		},
	}
	return obj
}

func TestAIGatewayCustomPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := validAIGatewayCustomPolicyInstalled(ns.Name)
		obj.Kind = "AIGatewayCustomPolicy"
		obj.APIVersion = aiconfigurationv1alpha1.GroupVersion.String()
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj).RunWithConfig(t, cfg, scheme)
	})

	t.Run("apiSpec validation", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayCustomPolicy]{
			{
				Name:       "installed variant is valid",
				TestObject: validAIGatewayCustomPolicyInstalled(ns.Name),
			},
			{
				Name: "explicit KonnectAIGateway ref is valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.AIGatewayRef.Kind = aiconfigurationv1alpha1.AIGatewayRefKindKonnect
					obj.Spec.AIGatewayRef.Group = aiconfigurationv1alpha1.AIGatewayRefGroupKonnect
					return obj
				}(),
			},
			{
				Name: "OnPremAIGateway ref is rejected",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.AIGatewayRef.Kind = aiconfigurationv1alpha1.AIGatewayRefKindOnPrem
					obj.Spec.AIGatewayRef.Group = aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem
					return obj
				}(),
				ExpectedErrorMessage: new("spec.aiGatewayRef.kind must be one of: KonnectAIGateway"),
			},
			{
				Name:       "streaming variant is valid",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
			},
			{
				Name: "Lua sources longer than the default string limit are valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyStreaming(ns.Name)
					obj.Spec.APISpec.Streaming.Schema = inlineLua("-- " + strings.Repeat("a", 4096))
					obj.Spec.APISpec.Streaming.Handler = inlineLua("-- " + strings.Repeat("a", 4096))
					return obj
				}(),
			},
			{
				Name: "inline Lua sources longer than 256KiB are invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = inlineLua(strings.Repeat("a", 262145))
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.schema.value: Too long"),
			},
			{
				Name: "Lua sources from a ConfigMap are valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyStreaming(ns.Name)
					obj.Spec.APISpec.Streaming.Schema = luaFromConfigMap("schema.lua")
					obj.Spec.APISpec.Streaming.Handler = luaFromConfigMap("handler.lua")
					return obj
				}(),
			},
			{
				Name: "configMapRef type requires configMapRef",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = aiconfigurationv1alpha1.ConfigMapDataSource{
						Type: aiconfigurationv1alpha1.ConfigMapDataSourceTypeConfigMapRef,
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; configMapRef required when type=configMapRef"),
			},
			{
				Name: "inline type requires value",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = aiconfigurationv1alpha1.ConfigMapDataSource{
						Type:         aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
						ConfigMapRef: &aiconfigurationv1alpha1.ConfigMapDataSourceRef{Name: "cm", Key: "schema.lua"},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("value required when type=inline; configMapRef required when type=configMapRef"),
			},
			{
				Name: "value and configMapRef cannot both be set",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = luaFromConfigMap("schema.lua")
					obj.Spec.APISpec.Installed.Schema.Value = new("return {}")
					return obj
				}(),
				ExpectedErrorMessage: new("only one of value and configMapRef can be set"),
			},
			{
				Name: "configMapRef requires a key",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = luaFromConfigMap("")
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.schema.configMapRef.key"),
			},
			{
				Name: "installed variant requires schema",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = aiconfigurationv1alpha1.ConfigMapDataSource{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.schema: Required value"),
			},
			{
				Name: "streaming variant requires handler",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyStreaming(ns.Name)
					obj.Spec.APISpec.Streaming.Handler = aiconfigurationv1alpha1.ConfigMapDataSource{}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.streaming.handler: Required value"),
			},
			{
				Name: "name must match the entity identifier pattern",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Name = "invalid name"
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.name in body should match"),
			},
			{
				Name: "unknown type is invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Type = "unknown"
					return obj
				}(),
				ExpectedErrorMessage: new(`spec.apiSpec.type: Unsupported value: "unknown": supported values: "installed", "streaming"`),
			},
			{
				Name:       "installed name is immutable",
				TestObject: validAIGatewayCustomPolicyInstalled(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Installed.Name = "renamed-installed-policy"
				},
				ExpectedUpdateErrorMessage: new("name is immutable"),
			},
			{
				Name:       "streaming name is immutable",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Streaming.Name = "renamed-streaming-policy"
				},
				ExpectedUpdateErrorMessage: new("name is immutable"),
			},
			{
				Name: "installed variant with labels and managedBy is valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Labels = aiconfigurationv1alpha1.PublicLabels{"team": "ai"}
					obj.Spec.APISpec.Installed.ManagedBy = aiconfigurationv1alpha1.ManagedBy{"tool": "kong-operator"}
					return obj
				}(),
			},
			{
				Name: "streaming variant with labels and managedBy is valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyStreaming(ns.Name)
					obj.Spec.APISpec.Streaming.Labels = aiconfigurationv1alpha1.PublicLabels{"team": "ai"}
					obj.Spec.APISpec.Streaming.ManagedBy = aiconfigurationv1alpha1.ManagedBy{"tool": "kong-operator"}
					return obj
				}(),
			},
			{
				Name: "label values must match the label value pattern",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Labels = aiconfigurationv1alpha1.PublicLabels{"team": "-invalid"}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.labels.team in body should match"),
			},
			{
				Name:       "labels and managedBy can be updated",
				TestObject: validAIGatewayCustomPolicyInstalled(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Installed.Labels = aiconfigurationv1alpha1.PublicLabels{"team": "ai"}
					obj.Spec.APISpec.Installed.ManagedBy = aiconfigurationv1alpha1.ManagedBy{"tool": "kong-operator"}
				},
			},
			{
				Name:       "type cannot change from installed to streaming",
				TestObject: validAIGatewayCustomPolicyInstalled(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.AIGatewayCustomPolicyConfig = validAIGatewayCustomPolicyStreaming(ns.Name).Spec.APISpec.AIGatewayCustomPolicyConfig
				},
				ExpectedUpdateErrorMessage: new("type is immutable"),
			},
			{
				Name:       "type cannot change from streaming to installed",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.AIGatewayCustomPolicyConfig = validAIGatewayCustomPolicyInstalled(ns.Name).Spec.APISpec.AIGatewayCustomPolicyConfig
				},
				ExpectedUpdateErrorMessage: new("type is immutable"),
			},
			{
				Name:       "display name and Lua sources can be updated",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Streaming.DisplayName = "Renamed display"
					obj.Spec.APISpec.Streaming.Schema = inlineLua("return { name = \"updated\" }")
					obj.Spec.APISpec.Streaming.Handler = inlineLua("return { VERSION = \"1.0.0\" }")
				},
			},
			{
				Name:       "Lua sources can switch from inline to a ConfigMap",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Streaming.Schema = luaFromConfigMap("schema.lua")
					obj.Spec.APISpec.Streaming.Handler = luaFromConfigMap("handler.lua")
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
