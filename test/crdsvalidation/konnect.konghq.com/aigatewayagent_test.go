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

func validAIGatewayAgent(ns string) *konnectv1alpha1.AIGatewayAgent {
	return &konnectv1alpha1.AIGatewayAgent{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: konnectv1alpha1.AIGatewayAgentSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-ai-gw-cp",
				},
			},
			APISpec: konnectv1alpha1.AIGatewayAgentAPISpec{
				Name:        "test-agent",
				DisplayName: "Test Agent",
				Type:        "http",
				Config: konnectv1alpha1.AIGatewayAgentConfig{
					URL: "https://upstream.example.com",
				},
			},
		},
	}
}

func TestAIGatewayAgent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("apiSpec.policies validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayAgent]{
			{
				Name: "policies ref with name only is valid (kind defaulted)",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Name: "test-policy"},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.AIGatewayAgent) {
					require.Len(t, obj.Spec.APISpec.Policies, 1)
					assert.Equal(t, "AIGatewayPolicy", obj.Spec.APISpec.Policies[0].Kind)
				},
			},
			{
				Name: "policies ref with empty name is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Name: ""},
					}
					return obj
				}(),
				ExpectedErrorMessage: new("spec.apiSpec.policies[0].name in body should be at least 1 chars long"),
			},
			{
				Name: "policies ref with wrong kind is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{
						{Kind: "KongPlugin", Name: "test-policy"},
					}
					return obj
				}(),
				ExpectedErrorMessage: new(`spec.apiSpec.policies[0].kind: Unsupported value: "KongPlugin": supported values: "AIGatewayPolicy"`),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("apiSpec.access.acls validation", func(t *testing.T) {
		common.TestCasesGroup[*konnectv1alpha1.AIGatewayAgent]{
			{
				Name: "ACL ref without kind defaults to AIGatewayConsumerGroup",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Access = konnectv1alpha1.AIGatewayAgentAccess{
						Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
							Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
							Allow: &konnectv1alpha1.AIGatewayAllowACL{
								Allow: []konnectv1alpha1.AIGatewayACLRef{
									{Name: "consumer-a"},
								},
							},
						},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.AIGatewayAgent) {
					require.NotNil(t, obj.Spec.APISpec.Access.Acls)
					require.NotNil(t, obj.Spec.APISpec.Access.Acls.Allow)
					require.Len(t, obj.Spec.APISpec.Access.Acls.Allow.Allow, 1)
					assert.Equal(t, "AIGatewayConsumerGroup", obj.Spec.APISpec.Access.Acls.Allow.Allow[0].Kind)
				},
			},
			{
				Name: "ACL ref with kind outside enum is invalid",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Access = konnectv1alpha1.AIGatewayAgentAccess{
						Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
							Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeDeny,
							Deny: &konnectv1alpha1.AIGatewayDenyACL{
								Deny: []konnectv1alpha1.AIGatewayACLRef{
									{Kind: "KongConsumer", Name: "consumer-a"},
								},
							},
						},
					}
					return obj
				}(),
				ExpectedErrorMessage: new(`spec.apiSpec.access.acls.deny.deny[0].kind: Unsupported value: "KongConsumer"`),
			},
			{
				Name: "ACL ref with valid kind and name is accepted",
				TestObject: func() *konnectv1alpha1.AIGatewayAgent {
					obj := validAIGatewayAgent(ns.Name)
					obj.Spec.APISpec.Access = konnectv1alpha1.AIGatewayAgentAccess{
						Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
							Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
							Allow: &konnectv1alpha1.AIGatewayAllowACL{
								Allow: []konnectv1alpha1.AIGatewayACLRef{
									{Kind: "AIGatewayConsumerGroup", Name: "group-a"},
								},
							},
						},
					}
					return obj
				}(),
				Assert: func(t *testing.T, obj *konnectv1alpha1.AIGatewayAgent) {
					require.NotNil(t, obj.Spec.APISpec.Access.Acls)
					require.NotNil(t, obj.Spec.APISpec.Access.Acls.Allow)
					require.Len(t, obj.Spec.APISpec.Access.Acls.Allow.Allow, 1)
					assert.Equal(t, "AIGatewayConsumerGroup", obj.Spec.APISpec.Access.Acls.Allow.Allow[0].Kind)
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
