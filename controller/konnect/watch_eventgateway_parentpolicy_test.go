package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEventGatewayParentPolicyWatch(t *testing.T) {
	t.Parallel()
	t.Run("produce", func(t *testing.T) {
		parent := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: "parent", Namespace: "default"}
		child := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
			Name: "child", Namespace: "default",
			Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
					ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: "parent"},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(parent, child).
			WithIndex(child, index.IndexFieldEventGatewayVirtualClusterProducePolicyOnEventGatewayVirtualClusterProducePolicyRef, eventGatewayProduceParentIndex).Build()
		enqueue := enqueueEventGatewayVirtualClusterProducePolicyForEventGatewayVirtualClusterProducePolicy(cl)
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(child)}}, enqueue(t.Context(), parent))
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(parent)}}, enqueue(t.Context(), child))
		require.NoError(t, cl.Delete(t.Context(), child))
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(parent)}}, enqueue(t.Context(), child))
	})
	t.Run("consume", func(t *testing.T) {
		parent := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: "parent", Namespace: "default"}
		child := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
			Name: "child", Namespace: "default",
			Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
					ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: "parent"},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(parent, child).
			WithIndex(child, index.IndexFieldEventGatewayVirtualClusterConsumePolicyOnEventGatewayVirtualClusterConsumePolicyRef, eventGatewayConsumeParentIndex).Build()
		enqueue := enqueueEventGatewayVirtualClusterConsumePolicyForEventGatewayVirtualClusterConsumePolicy(cl)
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(child)}}, enqueue(t.Context(), parent))
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(parent)}}, enqueue(t.Context(), child))
		require.NoError(t, cl.Delete(t.Context(), child))
		require.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(parent)}}, enqueue(t.Context(), child))
	})
}

func eventGatewayProduceParentIndex(obj client.Object) []string {
	p := obj.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy)
	var keys []string
	for _, key := range configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRefsToEventGatewayVirtualClusterProducePolicy(p) {
		keys = append(keys, key.String())
	}
	return keys
}

func eventGatewayConsumeParentIndex(obj client.Object) []string {
	p := obj.(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy)
	var keys []string
	for _, key := range configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRefsToEventGatewayVirtualClusterConsumePolicy(p) {
		keys = append(keys, key.String())
	}
	return keys
}
