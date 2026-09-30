package crdsvalidation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// aiGatewayPolicyWithoutType returns a valid AIGatewayPolicy except that
// neither spec.apiSpec.type nor spec.apiSpec.customPolicyRef is set.
func aiGatewayPolicyWithoutType(ns string) *aiconfigurationv1alpha1.AIGatewayPolicy {
	return &aiconfigurationv1alpha1.AIGatewayPolicy{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "aigateway-1",
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
				Name:        "policy1",
				DisplayName: "Test Policy",
				Config: aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
					Type:  "inline",
					Value: &apiextensionsv1.JSON{Raw: []byte(`{}`)},
				},
			},
		},
	}
}

func TestAIGatewayPolicyCustomPolicyRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	const exactlyOne = "exactly one of spec.apiSpec.type and spec.apiSpec.customPolicyRef must be set"

	common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayPolicy]{
		{
			Name: "type alone is valid",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "response-transformer"
				return obj
			}(),
		},
		{
			Name: "customPolicyRef alone is valid and defaults its kind",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
				return obj
			}(),
			Assert: func(t *testing.T, obj *aiconfigurationv1alpha1.AIGatewayPolicy) {
				assert.Equal(t, "AIGatewayCustomPolicy", obj.Spec.APISpec.CustomPolicyRef.Kind)
			},
		},
		{
			Name: "type and customPolicyRef together are rejected",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "my-custom-policy"
				obj.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
				return obj
			}(),
			ExpectedErrorMessage: new(exactlyOne),
		},
		{
			Name:                 "neither type nor customPolicyRef is rejected",
			TestObject:           aiGatewayPolicyWithoutType(ns.Name),
			ExpectedErrorMessage: new(exactlyOne),
		},
		{
			Name: "customPolicyRef to another kind is rejected",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{
					Kind: "AIGatewayPolicy",
					Name: "my-custom-policy",
				}
				return obj
			}(),
			ExpectedErrorMessage: new(`spec.apiSpec.customPolicyRef.kind: Unsupported value: "AIGatewayPolicy": supported values: "AIGatewayCustomPolicy"`),
		},
		{
			Name: "switching from type to customPolicyRef is allowed",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "my-custom-policy"
				return obj
			}(),
			Update: func(obj *aiconfigurationv1alpha1.AIGatewayPolicy) {
				obj.Spec.APISpec.Type = ""
				obj.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
			},
		},
	}.RunWithConfig(t, cfg, scheme)
}
