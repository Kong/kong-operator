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
			Name:       "parent cannot change before programming",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}...),
			Update: func(obj *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) {
				obj.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other"
			},
			ExpectedUpdateErrorMessage: new("spec.eventGatewayVirtualClusterRef is immutable"),
		},
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
			Name: "explicit same namespace",
			TestObject: chain(configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{
				Name:      "policy",
				Namespace: ns.Name,
			}),
		},
		{
			Name:       "programmed status does not require a chain ID",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) {
				obj.Status.Conditions = []metav1.Condition{{
					Type:               "Programmed",
					Status:             metav1.ConditionTrue,
					Reason:             "Programmed",
					Message:            "The complete order has been applied",
					ObservedGeneration: obj.Generation,
					LastTransitionTime: metav1.Now(),
				}}
			},
		},
		{
			Name:       "chain ID cannot be set",
			TestObject: chain([]configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}...),
			StatusUpdate: func(obj *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) {
				obj.Status.ID = "00000000-0000-4000-8000-000000000000"
			},
			ExpectedStatusUpdateErrorMessage: new("status.id must be unset because policy chains have no independent Konnect ID"),
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

func TestEventGatewayVirtualClusterPolicyChainRequiredFields(t *testing.T) {
	t.Parallel()

	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, t.Context(), scheme)
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	for _, tt := range []struct {
		name   string
		mutate func(*testing.T, *unstructured.Unstructured)
		field  string
	}{
		{
			name: "missing spec",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				delete(obj.Object, "spec")
			},
			field: "spec",
		},
		{
			name: "null spec",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				obj.Object["spec"] = nil
			},
			field: "spec",
		},
		{
			name: "missing apiSpec",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec")
			},
			field: "apiSpec",
		},
		{
			name: "null apiSpec",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(obj.Object, nil, "spec", "apiSpec"))
			},
			field: "apiSpec",
		},
		{
			name: "missing policies",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				unstructured.RemoveNestedField(obj.Object, "spec", "apiSpec", "policies")
			},
			field: "policies",
		},
		{
			name: "null policies",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(obj.Object, nil, "spec", "apiSpec", "policies"))
			},
			field: "policies",
		},
		{
			name: "parent cannot use a Konnect ID",
			mutate: func(t *testing.T, obj *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(obj.Object, map[string]any{
					"type":      "konnectID",
					"konnectID": "00000000-0000-4000-8000-000000000000",
				}, "spec", "eventGatewayVirtualClusterRef"))
			},
			field: "spec.eventGatewayVirtualClusterRef must use namespacedRef",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": configurationv1alpha1.GroupVersion.String(),
				"kind":       "EventGatewayVirtualClusterPolicyChain",
				"metadata": map[string]any{
					"namespace":    ns.Name,
					"generateName": "policy-chain-",
				},
				"spec": map[string]any{
					"eventGatewayVirtualClusterRef": map[string]any{
						"type":          "namespacedRef",
						"namespacedRef": map[string]any{"name": "virtual-cluster"},
					},
					"apiSpec": map[string]any{"policies": []any{}},
				},
			}}
			tt.mutate(t, obj)
			err := cl.Create(t.Context(), obj)
			require.True(t, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
			require.ErrorContains(t, err, tt.field)
		})
	}
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
