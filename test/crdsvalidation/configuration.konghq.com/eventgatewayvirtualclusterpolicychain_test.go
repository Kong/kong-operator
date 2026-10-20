package configuration_test

import (
	"testing"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterPolicyChain(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	chain := func(policies ...configurationv1alpha1.EventGatewayVirtualClusterPolicyRef) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
		return &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "virtual-cluster"},
				},
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainAPISpec{
					Policies: policies,
				},
			},
		}
	}

	common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain]{
		{
			Name: "ordered policy references",
			TestObject: chain(
				configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: "second"},
				configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: "first"},
			),
		},
		{
			Name:       "explicit empty policy list",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}...),
		},
		{
			Name: "wrong policy kind",
			TestObject: chain(configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{
				Kind: "EventGatewayVirtualClusterProducePolicy",
				Name: "policy",
			}),
			ExpectedErrorMessage: new(`supported values: "EventGatewayVirtualClusterPolicy"`),
		},
		{
			Name:                 "empty policy name",
			TestObject:           chain(configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}),
			ExpectedErrorMessage: new("should be at least 1 chars long"),
		},
	}.RunWithConfig(t, cfg, scheme)
}
