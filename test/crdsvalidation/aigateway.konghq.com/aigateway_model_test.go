package crdsvalidation_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayModel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("cp ref", func(t *testing.T) {
		obj := &konnectv1alpha1.AIGatewayModel{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AIGatewayModel",
				APIVersion: konnectv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: konnectv1alpha1.AIGatewayModelSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: konnectv1alpha1.AIGatewayModelAPISpec{
					AIGatewayModelConfig: &konnectv1alpha1.AIGatewayModelConfig{
						Type: konnectv1alpha1.AIGatewayModelConfigTypeModel,
						Model: &konnectv1alpha1.AIGatewayModelModel{
							Name:         "model1",
							DisplayName:  "Test Model",
							Capabilities: []string{"generate"},
							Formats:      []konnectv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
							Config: konnectv1alpha1.AIGatewayModelModelConfig{
								Route: konnectv1alpha1.AIGatewayModelRouteConfig{
									Paths: []string{"/v1/chat/completions"},
								},
							},
							Targets: []konnectv1alpha1.AIGatewayTarget{
								{
									Name:     "target1",
									Provider: konnectv1alpha1.AIGatewayModelProviderRef{Name: "modelprovider-1"},
									Config: &konnectv1alpha1.AIGatewayTargetConfig{
										Type:   konnectv1alpha1.AIGatewayTargetConfigTypeOpenai,
										Openai: &konnectv1alpha1.AIGatewayTargetOpenaiConfig{},
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
