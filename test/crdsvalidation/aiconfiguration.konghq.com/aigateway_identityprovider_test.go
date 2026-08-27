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

func TestAIGatewayAuthStrategy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
			TypeMeta: metav1.TypeMeta{
				Kind:       "AIGatewayAuthStrategy",
				APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
					AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
						Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeKeyAuth,
						KeyAuth: &aiconfigurationv1alpha1.AIGatewayAuthStrategyKeyAuth{
							Name:        "provider1",
							DisplayName: "Test Identity Provider",
						},
					},
				},
			},
		}
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})
}
