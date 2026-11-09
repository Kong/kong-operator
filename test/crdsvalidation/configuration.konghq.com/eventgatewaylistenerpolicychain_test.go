package configuration_test

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestEventGatewayListenerPolicyChain(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	chain := func(policies ...configurationv1alpha1.EventGatewayListenerPolicyRef) *configurationv1alpha1.EventGatewayListenerPolicyChain {
		return &configurationv1alpha1.EventGatewayListenerPolicyChain{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: configurationv1alpha1.EventGatewayListenerPolicyChainSpec{
				EventGatewayListenerRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "listener"},
				},
				APISpec: configurationv1alpha1.EventGatewayListenerPolicyChainAPISpec{Policies: policies},
			},
		}
	}
	common.TestCasesGroup[*configurationv1alpha1.EventGatewayListenerPolicyChain]{
		{
			Name:       "parent name cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayListenerPolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayListenerPolicyChain) {
				obj.Spec.EventGatewayListenerRef.NamespacedRef.Name = "other"
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayListenerRef is immutable"),
		},
		{
			Name:       "parent namespace cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayListenerPolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayListenerPolicyChain) {
				obj.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("other")
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayListenerRef is immutable"),
		},
		{
			Name: "ordered policy references",
			TestObject: chain(
				configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "second"},
				configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "first"},
			),
		},
		{Name: "explicit empty policy list", TestObject: chain([]configurationv1alpha1.EventGatewayListenerPolicyRef{}...)},
		{
			Name:       "explicit kind and same namespace",
			TestObject: chain(configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "policy", Kind: "EventGatewayListenerPolicy", Namespace: ns.Name}),
		},
		{
			Name:       "programmed without independent ID",
			TestObject: chain([]configurationv1alpha1.EventGatewayListenerPolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayListenerPolicyChain) {
				obj.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(obj.Generation)}
			},
		},
		{
			Name:       "independent ID rejected",
			TestObject: chain([]configurationv1alpha1.EventGatewayListenerPolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayListenerPolicyChain) {
				obj.Status.ID = "00000000-0000-4000-8000-000000000000"
			},
			ExpectedStatusUpdateErrorMessage: new("status.id must be unset because policy chains have no independent Konnect ID"),
		},
		{
			Name:                 "wrong policy kind",
			TestObject:           chain(configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "policy", Kind: "EventGatewayVirtualClusterPolicy"}),
			ExpectedErrorMessage: new(`supported values: "EventGatewayListenerPolicy"`),
		},
		{
			Name:                 "empty policy name",
			TestObject:           chain(configurationv1alpha1.EventGatewayListenerPolicyRef{}),
			ExpectedErrorMessage: new("should be at least 1 chars long"),
		},
	}.RunWithConfig(t, cfg, scheme)

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	obj := chain(configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "second"}, configurationv1alpha1.EventGatewayListenerPolicyRef{Name: "first"})
	require.NoError(t, cl.Create(t.Context(), obj))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.Equal(t, []configurationv1alpha1.EventGatewayListenerPolicyRef{
		{Name: "second", Kind: "EventGatewayListenerPolicy"},
		{Name: "first", Kind: "EventGatewayListenerPolicy"},
	}, obj.Spec.APISpec.Policies, "admission defaults kind without changing order or storing another namespace")
	old := obj.DeepCopy()
	obj.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "first"}, {Name: "second"}}
	require.NoError(t, cl.Patch(t.Context(), obj, client.MergeFrom(old)))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.Equal(t, []string{"first", "second"}, []string{obj.Spec.APISpec.Policies[0].Name, obj.Spec.APISpec.Policies[1].Name})
	old = obj.DeepCopy()
	obj.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{}
	require.NoError(t, cl.Patch(t.Context(), obj, client.MergeFrom(old)))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	require.NotNil(t, obj.Spec.APISpec.Policies)
	require.Empty(t, obj.Spec.APISpec.Policies, "an atomic replacement must retain the explicit empty list")
}

func TestEventGatewayListenerPolicyChainRequiredFields(t *testing.T) {
	t.Parallel()

	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	for _, field := range [][]string{
		{"spec"},
		{"spec", "apiSpec"},
		{"spec", "apiSpec", "policies"},
		{"spec", "eventGatewayListenerRef"},
	} {
		for _, null := range []bool{false, true} {
			name := field[len(field)-1] + "/missing"
			if null {
				name = field[len(field)-1] + "/null"
			}
			t.Run(name, func(t *testing.T) {
				obj := listenerChainUnstructured(ns.Name)
				if null {
					require.NoError(t, unstructured.SetNestedField(obj.Object, nil, field...))
				} else {
					unstructured.RemoveNestedField(obj.Object, field...)
				}
				err := cl.Create(t.Context(), obj)
				require.True(t, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
				require.ErrorContains(t, err, field[len(field)-1])
			})
		}
	}
	t.Run("parent by Konnect ID", func(t *testing.T) {
		obj := listenerChainUnstructured(ns.Name)
		require.NoError(t, unstructured.SetNestedField(obj.Object, map[string]any{
			"type": "konnectID", "konnectID": "00000000-0000-4000-8000-000000000000",
		}, "spec", "eventGatewayListenerRef"))
		err := cl.Create(t.Context(), obj)
		require.True(t, apierrors.IsInvalid(err))
		require.ErrorContains(t, err, "spec.eventGatewayListenerRef must use namespacedRef")
	})
}

func listenerChainUnstructured(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": configurationv1alpha1.GroupVersion.String(),
		"kind":       "EventGatewayListenerPolicyChain",
		"metadata":   map[string]any{"namespace": namespace, "generateName": "listener-chain-"},
		"spec": map[string]any{
			"eventGatewayListenerRef": map[string]any{
				"type": "namespacedRef", "namespacedRef": map[string]any{"name": "listener"},
			},
			"apiSpec": map[string]any{"policies": []any{}},
		},
	}}
}

func TestEventGatewayListenerPolicyChainSample(t *testing.T) {
	t.Parallel()

	cfg, ns := envtest.Setup(t, t.Context(), scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	file, err := os.Open("../../../config/samples/konnect_eventgateway_listener_policy_chain.yaml")
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
		"KonnectAPIAuthConfiguration/listener-chain-auth",
		"KonnectEventGateway/listener-chain-gateway",
		"EventGatewayListener/listener-chain-listener",
		"EventGatewayBackendCluster/listener-chain-backend",
		"EventGatewayVirtualCluster/listener-chain-virtual-cluster",
		"EventGatewayListenerPolicy/listener-chain-forward",
		"EventGatewayListenerPolicyChain/listener-chain-example",
	}, resources)
	var chain configurationv1alpha1.EventGatewayListenerPolicyChain
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "listener-chain-example"}, &chain))
	require.Equal(t, []configurationv1alpha1.EventGatewayListenerPolicyRef{
		{Name: "listener-chain-forward", Kind: "EventGatewayListenerPolicy", Namespace: "default"},
	}, chain.Spec.APISpec.Policies)
	require.NotNil(t, chain.Spec.EventGatewayListenerRef.NamespacedRef)
	require.Equal(t, "listener-chain-listener", chain.Spec.EventGatewayListenerRef.NamespacedRef.Name)
	var policy configurationv1alpha1.EventGatewayListenerPolicy
	require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: ns.Name, Name: "listener-chain-forward"}, &policy))
	require.Equal(t, chain.Spec.EventGatewayListenerRef, policy.Spec.EventGatewayListenerRef)
	require.NotNil(t, policy.Spec.APISpec.EventGatewayListenerPolicyConfig)
	require.Equal(t, configurationv1alpha1.EventGatewayListenerPolicyConfigTypeForwardToVirtualClust, policy.Spec.APISpec.Type)
	forward := policy.Spec.APISpec.ForwardToVirtualClust
	require.NotNil(t, forward)
	require.NotNil(t, forward.Config)
	require.Equal(t, configurationv1alpha1.ForwardToVirtualClusterPolicyConfigTypePortMapping, forward.Config.Type)
	require.NotNil(t, forward.Config.PortMapping)
	require.NotNil(t, forward.Config.PortMapping.Destination)
	require.Equal(t, new(configurationv1alpha1.VirtualClusterName("listener-chain-virtual-cluster")), forward.Config.PortMapping.Destination.Name)
}
