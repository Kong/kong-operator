package crdsvalidation_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestAIGatewayPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("AI Gateway ref", func(t *testing.T) {
		obj := &aiconfigurationv1alpha1.AIGatewayPolicy{
			Kind:       "AIGatewayPolicy",
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name: "aigateway-1",
					},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
					Name:        "policy1",
					DisplayName: "Test Policy",
					Type:        "ai-prompt-decorator",
					Config: aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
						Type:  "inline",
						Value: &apiextensionsv1.JSON{Raw: []byte(`{}`)},
					},
				},
			},
		}
		common.NewCRDValidationTestCasesGroupParentRefChange(t, cfg, obj)
	})

	t.Run("apiSpec.config llm_format", func(t *testing.T) {
		// The policy config is free-form plugin configuration, so llm_format is
		// not schema-validated by the CRD: Kong is the source of truth for the
		// accepted values. This pins that new values such as "bedrock" are not
		// rejected at admission and survive the round trip untouched.
		policyWithConfig := func(config string) *aiconfigurationv1alpha1.AIGatewayPolicy {
			return &aiconfigurationv1alpha1.AIGatewayPolicy{
				Kind:       "AIGatewayPolicy",
				APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
				ObjectMeta: common.CommonObjectMeta(ns.Name),
				Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
					AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "aigateway-1"},
					},
					APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
						Name:        "policy1",
						DisplayName: "Test Policy",
						Type:        "ai-proxy-advanced",
						Config: aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
							Type:  "inline",
							Value: &apiextensionsv1.JSON{Raw: []byte(config)},
						},
					},
				},
			}
		}
		assertLLMFormat := func(want string) func(*testing.T, *aiconfigurationv1alpha1.AIGatewayPolicy) {
			return func(t *testing.T, obj *aiconfigurationv1alpha1.AIGatewayPolicy) {
				require.NotNil(t, obj.Spec.APISpec.Config.Value)
				var got struct {
					LLMFormat string `json:"llm_format"`
				}
				require.NoError(t, json.Unmarshal(obj.Spec.APISpec.Config.Value.Raw, &got))
				assert.Equal(t, want, got.LLMFormat)
			}
		}

		common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayPolicy]{
			{
				Name:       "llm_format bedrock is accepted",
				TestObject: policyWithConfig(`{"llm_format":"bedrock"}`),
				Assert:     assertLLMFormat("bedrock"),
			},
			{
				Name:       "llm_format openai is accepted",
				TestObject: policyWithConfig(`{"llm_format":"openai"}`),
				Assert:     assertLLMFormat("openai"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
