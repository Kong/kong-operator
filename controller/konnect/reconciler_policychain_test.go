package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func testPolicyChain(name string) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
	return &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
		Name: name, Namespace: "default", UID: types.UID(name + "-uid"),
		Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{
			EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{},
			},
		},
	}
}

func TestPolicyChainOwnership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	first, second := testPolicyChain("first"), testPolicyChain("second")
	parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default"}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
		WithStatusSubresource(first, second).WithObjects(parent, first, second).Build()

	owned, res, err := claimPolicyChain(ctx, cl, first)
	require.NoError(t, err)
	require.True(t, owned)
	require.True(t, res.IsZero())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, "first/first-uid", parent.Annotations[policyChainOwnerAnnotation])

	owned, res, err = claimPolicyChain(ctx, cl, second)
	require.NoError(t, err)
	require.False(t, owned)
	require.Positive(t, res.RequeueAfter)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(second), second))
	require.False(t, entityHasProgrammedStatusTrue(second))
	require.Contains(t, second.Status.Conditions[0].Message, "owned by policy chain first")

	require.NoError(t, cl.Delete(ctx, first))
	owned, res, err = claimPolicyChain(ctx, cl, second)
	require.NoError(t, err)
	require.True(t, owned)
	require.True(t, res.IsZero())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, "second/second-uid", parent.Annotations[policyChainOwnerAnnotation])
}

func TestPolicyChainSingletonReadiness(t *testing.T) {
	t.Parallel()
	chain := testPolicyChain("chain")
	require.True(t, shouldCreateKonnectEntity(chain))
	chain.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}}
	require.Empty(t, chain.GetKonnectID())
	require.False(t, shouldCreateKonnectEntity(chain))
}
