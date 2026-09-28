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
						Schema:      "return {}",
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
			Schema:      "return {}",
			Handler:     "return {}",
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
					obj.Spec.APISpec.Streaming.Schema = "-- " + strings.Repeat("a", 4096)
					obj.Spec.APISpec.Streaming.Handler = "-- " + strings.Repeat("a", 4096)
					return obj
				}(),
			},
			{
				Name: "installed variant requires schema",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyInstalled(ns.Name)
					obj.Spec.APISpec.Installed.Schema = ""
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.installed.schema: Required value"),
			},
			{
				Name: "streaming variant requires handler",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
					obj := validAIGatewayCustomPolicyStreaming(ns.Name)
					obj.Spec.APISpec.Streaming.Handler = ""
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
				Name:       "display name and Lua sources can be updated",
				TestObject: validAIGatewayCustomPolicyStreaming(ns.Name),
				Update: func(obj *aiconfigurationv1alpha1.AIGatewayCustomPolicy) {
					obj.Spec.APISpec.Streaming.DisplayName = "Renamed display"
					obj.Spec.APISpec.Streaming.Schema = "return { name = \"updated\" }"
					obj.Spec.APISpec.Streaming.Handler = "return { VERSION = \"1.0.0\" }"
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
