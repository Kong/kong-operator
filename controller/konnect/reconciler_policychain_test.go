package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
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

func TestPolicyChainRetainOnDeletion(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		owner  string
		parent bool
	}{
		{name: "release owner", owner: "chain/chain-uid", parent: true},
		{name: "preserve other owner", owner: "other/other-uid", parent: true},
		{name: "missing parent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			chain := testPolicyChain("chain")
			chain.DeletionTimestamp = new(metav1.Now())
			controllerutil.AddFinalizer(chain, KonnectCleanupFinalizer)
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default",
				Annotations: map[string]string{policyChainOwnerAnnotation: tt.owner},
			}
			clBuilder := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(chain)
			if tt.parent {
				clBuilder = clBuilder.WithObjects(parent)
			}
			cl := clBuilder.Build()
			// No SDK factory or authentication: deletion must stay entirely local.
			r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](nil, logging.DevelopmentMode, cl)
			res, err := r.Reconcile(ctx, chain)
			require.NoError(t, err)
			require.True(t, res.IsZero())
			require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
			if tt.parent {
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
				if tt.owner == "chain/chain-uid" {
					require.NotContains(t, parent.Annotations, policyChainOwnerAnnotation)
				} else {
					require.Equal(t, tt.owner, parent.Annotations[policyChainOwnerAnnotation])
				}
			}
		})
	}
}
