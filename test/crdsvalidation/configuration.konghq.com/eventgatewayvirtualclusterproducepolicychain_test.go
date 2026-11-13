package configuration_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayVirtualClusterProducePolicyChain(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	chain := func(policies ...configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain {
		return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "virtual-cluster"},
				},
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{Policies: policies},
			},
		}
	}
	common.TestCasesGroup[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain]{
		{
			Name:       "parent cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain) {
				obj.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayVirtualClusterRef is immutable"),
		},
		{Name: "explicit empty policy list", TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}...)},
		{
			Name:       "explicit kind and same namespace",
			TestObject: chain(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: "policy", Kind: "EventGatewayVirtualClusterProducePolicy", Namespace: ns.Name}),
		},
		{
			Name:       "independent ID rejected",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain) {
				obj.Status.ID = "00000000-0000-4000-8000-000000000000"
			},
			ExpectedStatusUpdateErrorMessage: new("status.id must be unset because policy chains have no independent Konnect ID"),
		},
		{
			Name:                 "wrong policy kind",
			TestObject:           chain(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: "policy", Kind: "EventGatewayVirtualClusterConsumePolicy"}),
			ExpectedErrorMessage: new(`supported values: "EventGatewayVirtualClusterProducePolicy"`),
		},
		{
			Name:                 "empty policy name",
			TestObject:           chain(configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}),
			ExpectedErrorMessage: new("should be at least 1 chars long"),
		},
	}.RunWithConfig(t, cfg, scheme)

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	for _, field := range [][]string{
		{"spec"}, {"spec", "apiSpec"}, {"spec", "apiSpec", "policies"}, {"spec", "eventGatewayVirtualClusterRef"},
	} {
		for _, null := range []bool{false, true} {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": configurationv1alpha1.GroupVersion.String(),
				"kind":       "EventGatewayVirtualClusterProducePolicyChain",
				"metadata":   map[string]any{"namespace": ns.Name, "generateName": "produce-chain-"},
				"spec": map[string]any{
					"eventGatewayVirtualClusterRef": map[string]any{
						"type": "namespacedRef", "namespacedRef": map[string]any{"name": "virtual-cluster"},
					},
					"apiSpec": map[string]any{"policies": []any{}},
				},
			}}
			if null {
				require.NoError(t, unstructured.SetNestedField(obj.Object, nil, field...))
			} else {
				unstructured.RemoveNestedField(obj.Object, field...)
			}
			err := cl.Create(t.Context(), obj)
			require.True(t, apierrors.IsInvalid(err), "field %v null=%t: %v", field, null, err)
			require.ErrorContains(t, err, field[len(field)-1])
		}
	}
}

func TestEventGatewayVirtualClusterProducePolicyChainSample(t *testing.T) {
	t.Parallel()

	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	file, err := os.Open("../../../config/samples/konnect_eventgateway_virtualcluster_produce_policy_chain.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	var resources []string
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(obj)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		obj.SetNamespace(ns.Name)
		require.NoError(t, cl.Create(t.Context(), obj), "%s %s", obj.GetKind(), obj.GetName())
		resources = append(resources, obj.GetKind()+"/"+obj.GetName())
	}
	require.ElementsMatch(t, []string{
		"KonnectAPIAuthConfiguration/produce-chain-auth",
		"KonnectEventGateway/produce-chain-gateway",
		"EventGatewayBackendCluster/produce-chain-backend",
		"EventGatewayVirtualCluster/produce-chain-virtual-cluster",
		"EventGatewayVirtualClusterProducePolicy/produce-chain-parse",
		"EventGatewayVirtualClusterProducePolicy/produce-chain-child-first",
		"EventGatewayVirtualClusterProducePolicy/produce-chain-child-second",
		"EventGatewayVirtualClusterProducePolicy/produce-chain-root",
		"EventGatewayVirtualClusterProducePolicyChain/produce-chain-example",
	}, resources)
	var chain configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "produce-chain-example"}, &chain))
	require.Equal(t, []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{
		{Name: "produce-chain-parse", Kind: "EventGatewayVirtualClusterProducePolicy"},
		{Name: "produce-chain-child-second", Kind: "EventGatewayVirtualClusterProducePolicy"},
		{Name: "produce-chain-child-first", Kind: "EventGatewayVirtualClusterProducePolicy"},
		{Name: "produce-chain-root", Kind: "EventGatewayVirtualClusterProducePolicy"},
	}, chain.Spec.APISpec.Policies, "admission defaults kind and preserves the complete nested-aware order")
	for _, name := range []string{"produce-chain-child-first", "produce-chain-child-second"} {
		var child configurationv1alpha1.EventGatewayVirtualClusterProducePolicy
		require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: name}, &child))
		require.NotNil(t, child.Spec.APISpec.ParentPolicyRef)
		require.Equal(t, "produce-chain-parse", child.Spec.APISpec.ParentPolicyRef.Name)
		require.Equal(t, chain.GetParentRef(), child.GetParentRef())
	}
}
