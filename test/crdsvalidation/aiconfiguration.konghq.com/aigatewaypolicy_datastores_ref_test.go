package crdsvalidation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestAIGatewayPolicyDatastoresRef verifies that spec.apiSpec.datastores takes
// AIGatewayDatastore references.
func TestAIGatewayPolicyDatastoresRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	common.TestCasesGroup[*aiconfigurationv1alpha1.AIGatewayPolicy]{
		{
			Name: "a datastore reference is valid and defaults its kind",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "response-transformer"
				obj.Spec.APISpec.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{
					{Name: "my-datastore"},
				}
				return obj
			}(),
			Assert: func(t *testing.T, obj *aiconfigurationv1alpha1.AIGatewayPolicy) {
				assert.Equal(t, "AIGatewayDatastore", obj.Spec.APISpec.Datastores[0].Kind)
			},
		},
		{
			Name: "a datastore reference to another kind is rejected",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "response-transformer"
				obj.Spec.APISpec.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{
					{Kind: "AIGatewayPolicy", Name: "my-datastore"},
				}
				return obj
			}(),
			ExpectedErrorMessage: new(`spec.apiSpec.datastores[0].kind: Unsupported value: "AIGatewayPolicy": supported values: "AIGatewayDatastore"`),
		},
		{
			Name: "a datastore reference without a name is rejected",
			TestObject: func() *aiconfigurationv1alpha1.AIGatewayPolicy {
				obj := aiGatewayPolicyWithoutType(ns.Name)
				obj.Spec.APISpec.Type = "response-transformer"
				obj.Spec.APISpec.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{{}}
				return obj
			}(),
			ExpectedErrorMessage: new("spec.apiSpec.datastores[0].name"),
		},
	}.RunWithConfig(t, cfg, scheme)
}
