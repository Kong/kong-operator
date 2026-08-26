package crdsvalidation_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayModelProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayModelProvider{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AIGatewayModelProvider",
				APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayModelProviderSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayModelProviderAPISpec{
					AIGatewayModelProviderConfig: &aiconfigurationv1alpha1.AIGatewayModelProviderConfig{
						Type: aiconfigurationv1alpha1.AIGatewayModelProviderConfigTypeOpenai,
						Openai: &aiconfigurationv1alpha1.AIGatewayModelProviderOpenai{
							Name:        "modelprovider1",
							DisplayName: "Test Model Provider",
							Config: aiconfigurationv1alpha1.AIGatewayModelProviderOpenaiConfig{
								Auth: aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasic{
									Headers: []aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasicHeaders{
										{
											Name: "Authorization",
											Value: aiconfigurationv1alpha1.SensitiveDataSource{
												Type:  "inline",
												Value: new("sk-test"),
											},
										},
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
