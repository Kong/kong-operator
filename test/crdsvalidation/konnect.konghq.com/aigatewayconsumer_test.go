package crdsvalidation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validAIGatewayConsumer(ns string) *konnectv1alpha1.AIGatewayConsumer {
	return &konnectv1alpha1.AIGatewayConsumer{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.AIGatewayConsumerSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-ai-gw-cp",
				},
			},
			APISpec: konnectv1alpha1.AIGatewayConsumerAPISpec{
				Name:        "test-consumer",
				DisplayName: "Test Consumer",
				Type:        "api-key",
			},
		},
	}
}

func TestAIGatewayConsumer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("apiSpec.policies validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayConsumer]{
			{
				Name: "policies ref with name only is valid (kind defaulted)",
				TestObject: func() *konnectv1alpha1.AIGatewayConsumer {
					obj := validAIGatewayConsumer(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Name: "test-policy"},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.AIGatewayConsumer) {
					require.Len(t, obj.Spec.APISpec.Policies, 1)
					assert.Equal(t, "AIGatewayPolicy", obj.Spec.APISpec.Policies[0].Kind)
				},
			},
			{
				Name: "policies ref with empty name is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayConsumer {
					obj := validAIGatewayConsumer(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Name: ""},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.policies[0].name in body should be at least 1 chars long"),
			},
			{
				Name: "policies ref with wrong kind is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayConsumer {
					obj := validAIGatewayConsumer(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Kind: "KongPlugin", Name: "test-policy"},
					}
					return obj
				}(),
				ExpectedErrorMessage: new(`spec.apiSpec.policies[0].kind: Unsupported value: "KongPlugin": supported values: "AIGatewayPolicy"`),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("spec.consumerGroups validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayConsumer]{
			{
				Name: "consumerGroups ref with name is valid",
				TestObject: func() *konnectv1alpha1.AIGatewayConsumer {
					obj := validAIGatewayConsumer(ns.Name)
					obj.Spec.ConsumerGroups = []konnectv1alpha1.AIGatewayConsumerGroupRef{
						{Name: "test-group"},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.AIGatewayConsumer) {
					require.Len(t, obj.Spec.ConsumerGroups, 1)
					assert.Equal(t, "test-group", obj.Spec.ConsumerGroups[0].Name)
				},
			},
			{
				Name: "consumerGroups ref with empty name is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayConsumer {
					obj := validAIGatewayConsumer(ns.Name)
					obj.Spec.ConsumerGroups = []konnectv1alpha1.AIGatewayConsumerGroupRef{
						{Name: ""},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.consumerGroups[0].name in body should be at least 1 chars long"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
