package crdsvalidation

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterProducePolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("valid object", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.EventGatewayVirtualClusterProducePolicy]{
			{
				Name: "minimal valid object",
				TestObject: &konnectv1alpha1.EventGatewayVirtualClusterProducePolicy{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					Spec: konnectv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
						EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "my-event-gateway-virtual-cluster",
							},
						},
						APISpec: konnectv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
							EventGatewayVirtualClusterProducePolicyConfig: &konnectv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
								Type: konnectv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
								ModifyHeadersPolicyCreate: &konnectv1alpha1.EventGatewayModifyHeadersPolicyCreate{
									Config: konnectv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
										Actions: []konnectv1alpha1.EventGatewayModifyHeaderAction{
											{
												Op: konnectv1alpha1.EventGatewayModifyHeaderActionTypeSet,
												Set: &konnectv1alpha1.EventGatewayModifyHeaderSetAction{
													Key:   "x-produced-header",
													Value: "set-by-test",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
