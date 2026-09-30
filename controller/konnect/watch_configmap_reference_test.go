package konnect

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueObjectsForConfigMapRef(t *testing.T) {
	customPolicy := func(ns, name string, schema, handler aiconfigurationv1alpha1.ConfigMapDataSource) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
		return &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
			Namespace: ns, Name: name,
			Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
				AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "ai-gw"},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
						Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
						Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
							Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
							DisplayName: name,
							Schema:      schema,
							Handler:     handler,
						},
					},
				},
			},
		}
	}
	inline := aiconfigurationv1alpha1.ConfigMapDataSource{
		Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
		Value: new("return {}"),
	}
	fromConfigMap := func(name, key string) aiconfigurationv1alpha1.ConfigMapDataSource {
		return aiconfigurationv1alpha1.ConfigMapDataSource{
			Type:         aiconfigurationv1alpha1.ConfigMapDataSourceTypeConfigMapRef,
			ConfigMapRef: &aiconfigurationv1alpha1.ConfigMapDataSourceRef{Name: name, Key: key},
		}
	}

	onPrem := customPolicy("default", "on-prem", fromConfigMap("lua", "schema.lua"), inline)
	onPrem.Spec.AIGatewayRef.Kind = aiconfigurationv1alpha1.AIGatewayRefKindOnPrem
	onPrem.Spec.AIGatewayRef.Group = aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem

	objs := []client.Object{
		customPolicy("default", "schema-ref", fromConfigMap("lua", "schema.lua"), inline),
		customPolicy("default", "handler-ref", inline, fromConfigMap("lua", "handler.lua")),
		customPolicy("default", "other-configmap", fromConfigMap("other", "schema.lua"), inline),
		customPolicy("default", "inline-only", inline, inline),
		customPolicy("other-ns", "same-name-other-ns", fromConfigMap("lua", "schema.lua"), inline),
		onPrem,
	}
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(objs...).
		Build()

	f := enqueueObjectsForConfigMapRef[aiconfigurationv1alpha1.AIGatewayCustomPolicyList](cl)

	t.Run("enqueues objects referencing the ConfigMap in its namespace", func(t *testing.T) {
		got := f(t.Context(), &corev1.ConfigMap{Namespace: "default", Name: "lua"})
		require.ElementsMatch(t, []reconcile.Request{
			{Namespace: "default", Name: "schema-ref"},
			{Namespace: "default", Name: "handler-ref"},
		}, got)
	})

	t.Run("ignores unreferenced ConfigMaps", func(t *testing.T) {
		got := f(t.Context(), &corev1.ConfigMap{Namespace: "default", Name: "unused"})
		require.Empty(t, got)
	})

	t.Run("ignores non-ConfigMap objects", func(t *testing.T) {
		got := f(t.Context(), &corev1.Secret{Namespace: "default", Name: "lua"})
		require.Empty(t, got)
	})
}

// TestDataSourceRefDispatchCoversAllGeneratedTypes guards the per-API-group
// type switches in configMapRefsForDataSource and secretRefsForSensitiveData:
// a type generated with ConfigMap or Secret dataSources in a group the switch
// doesn't know would otherwise silently skip reference validation and the
// ConfigMap/Secret watches.
func TestDataSourceRefDispatchCoversAllGeneratedTypes(t *testing.T) {
	var configMapTypes, secretTypes int
	for gvk, typ := range scheme.Get().AllKnownTypes() {
		obj := reflect.New(typ).Interface()
		ptrType := reflect.TypeOf(obj)

		if _, ok := ptrType.MethodByName("GetConfigMapDataSourceRefs"); ok {
			configMapTypes++
			_, handled := configMapRefsForDataSource(obj)
			require.Truef(t, handled, "%s has GetConfigMapDataSourceRefs but configMapRefsForDataSource doesn't handle its API group", gvk)
		}
		if _, ok := ptrType.MethodByName("GetSensitiveDataSecretRefs"); ok {
			secretTypes++
			_, handled := secretRefsForSensitiveData(obj)
			require.Truef(t, handled, "%s has GetSensitiveDataSecretRefs but secretRefsForSensitiveData doesn't handle its API group", gvk)
		}
	}
	require.NotZero(t, configMapTypes, "expected at least one type with ConfigMap data sources")
	require.NotZero(t, secretTypes, "expected at least one type with Secret data sources")
}
