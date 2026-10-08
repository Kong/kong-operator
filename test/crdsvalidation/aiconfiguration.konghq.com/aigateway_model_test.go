package crdsvalidation_test

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validAIGatewayModel(ns string) *aiconfigurationv1alpha1.AIGatewayModel {
	return &aiconfigurationv1alpha1.AIGatewayModel{
		Kind:       "AIGatewayModel",
		APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "aigateway-1",
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:         "model1",
						DisplayName:  "Test Model",
						Capabilities: []string{"generate"},
						Formats:      []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{
								Paths: []string{"/v1/chat/completions"},
							},
						},
						Targets: []aiconfigurationv1alpha1.AIGatewayTarget{
							{
								Name:     "target1",
								Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{Name: "modelprovider-1"},
								Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
									Type:   aiconfigurationv1alpha1.AIGatewayTargetConfigTypeOpenai,
									Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestAIGatewayModel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, validAIGatewayModel(ns.Name))
	})

	t.Run("apiSpec.config.route.model.pathParam validation", func(t *testing.T) {
		// The path_param value is the plain name of the regex capture group
		// defined in the route path (Konnect used to require a "~" prefix,
		// dropped in the Konnect API revert shipped in sdk-konnect-go v0.73.1).
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayModel]{
			{
				Name: "pathParam with plain capture group name is valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					obj.Spec.APISpec.AIGatewayModelConfig.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
						PathParam: "model_name",
						Values:    []string{"my-alias"},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *aiconfigurationv1alpha1.AIGatewayModel) {
					if obj.Spec.APISpec.AIGatewayModelConfig.Model.Config.Route.Model.PathParam != "model_name" {
						t.Errorf("expected pathParam model_name, got %q", obj.Spec.APISpec.AIGatewayModelConfig.Model.Config.Route.Model.PathParam)
					}
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("target config bounds validation", func(t *testing.T) {
		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayModel]{
			{
				Name: "target config values at bounds are valid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					cfg := obj.Spec.APISpec.AIGatewayModelConfig.Model.Targets[0].Config.Openai
					cfg.Temperature = 5
					cfg.TopK = 500
					cfg.TopP = 1
					cfg.InputCost = 0
					return obj
				}(),
			},
			{
				Name: "target config topK above 500 is invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					obj.Spec.APISpec.AIGatewayModelConfig.Model.Targets[0].Config.Openai.TopK = 501
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.model.targets[0].config.openai.topK in body should be less than or equal to 500"),
			},
			{
				Name: "target config temperature above 5 is invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					obj.Spec.APISpec.AIGatewayModelConfig.Model.Targets[0].Config.Openai.Temperature = 5.1
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.model.targets[0].config.openai.temperature in body should be less than or equal to 5"),
			},
			{
				Name: "target config topP above 1 is invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					obj.Spec.APISpec.AIGatewayModelConfig.Model.Targets[0].Config.Openai.TopP = 1.1
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.model.targets[0].config.openai.topP in body should be less than or equal to 1"),
			},
			{
				Name: "target config negative cost is invalid",
				TestObject: func() *aiconfigurationv1alpha1.AIGatewayModel {
					obj := validAIGatewayModel(ns.Name)
					obj.Spec.APISpec.AIGatewayModelConfig.Model.Targets[0].Config.Openai.InputCost = -1
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.model.targets[0].config.openai.inputCost in body should be greater than or equal to 0"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
