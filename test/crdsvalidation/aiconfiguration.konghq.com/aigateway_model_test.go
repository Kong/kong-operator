package crdsvalidation_test

import (
	"testing"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayModel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayModel{
			Kind:       "AIGatewayModel",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
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
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})
}
