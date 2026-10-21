package configuration_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func TestEventGatewayVirtualClusterPolicyChainSample(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	file, err := os.Open("../../../config/samples/konnect_eventgateway_virtualcluster_policy_chain.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		obj.SetNamespace(ns.Name)
		require.NoError(t, cl.Create(t.Context(), obj), "%s %s", obj.GetKind(), obj.GetName())
	}

	var chain configurationv1alpha1.EventGatewayVirtualClusterPolicyChain
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "policy-chain-example"}, &chain))
	require.Len(t, chain.Spec.APISpec.Policies, 2)
	require.Equal(t, []string{"policy-chain-deny-admin-topics", "policy-chain-allow-orders-topics"}, []string{
		chain.Spec.APISpec.Policies[0].Name,
		chain.Spec.APISpec.Policies[1].Name,
	})
	for _, ref := range chain.Spec.APISpec.Policies {
		var policy configurationv1alpha1.EventGatewayVirtualClusterPolicy
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: ref.Name}, &policy))
		require.Equal(t, chain.Spec.EventGatewayVirtualClusterRef, policy.Spec.EventGatewayVirtualClusterRef)
	}
}
