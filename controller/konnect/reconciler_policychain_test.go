package konnect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
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

func TestPolicyChainOwnershipStaleAnnotation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name        string
		owner       string
		lookupOwner bool
	}{
		{name: "missing separator", owner: "garbage"},
		{name: "missing name", owner: "/other-uid"},
		{name: "missing UID", owner: "other/"},
		{name: "missing name and UID", owner: "/"},
		{name: "missing owner", owner: "missing/missing-uid", lookupOwner: true},
		{name: "recreated owner", owner: "other/old-uid", lookupOwner: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			chain, other := testPolicyChain("chain"), testPolicyChain("other")
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{
				Name: "vc", Namespace: "default",
				Annotations: map[string]string{policyChainOwnerAnnotation: tt.owner},
			}
			lookedUpOwner := false
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
				WithStatusSubresource(chain).WithObjects(parent, chain, other).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain); ok {
							lookedUpOwner = true
							require.True(t, tt.lookupOwner, "malformed ownership must not trigger an owner lookup")
						}
						return cl.Get(ctx, key, obj, opts...)
					},
				}).Build()

			owned, res, err := claimPolicyChain(ctx, cl, chain)
			require.NoError(t, err)
			require.True(t, owned)
			require.True(t, res.IsZero())
			require.Equal(t, tt.lookupOwner, lookedUpOwner)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
			require.Equal(t, "chain/chain-uid", parent.Annotations[policyChainOwnerAnnotation])
			require.True(t, apimeta.IsStatusConditionTrue(chain.Status.Conditions, "PolicyChainOwnerValid"))
		})
	}
}

func TestPolicyChainOwnershipLookupError(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	chain := testPolicyChain("chain")
	parent := &configurationv1alpha1.EventGatewayVirtualCluster{
		Name: "vc", Namespace: "default",
		Annotations: map[string]string{policyChainOwnerAnnotation: "other/other-uid"},
	}
	failure := errors.New("owner lookup unavailable")
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
		WithStatusSubresource(chain).WithObjects(parent, chain).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain); ok {
					return failure
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()

	owned, res, err := claimPolicyChain(ctx, cl, chain)
	require.ErrorIs(t, err, failure)
	require.False(t, owned)
	require.True(t, res.IsZero())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, "other/other-uid", parent.Annotations[policyChainOwnerAnnotation])
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
		ref    *commonv1alpha1.ObjectRef
	}{
		{name: "release owner", owner: "chain/chain-uid", parent: true},
		{name: "preserve other owner", owner: "other/other-uid", parent: true},
		{name: "missing parent"},
		{name: "cross-namespace parent", owner: "chain/chain-uid", parent: true, ref: &commonv1alpha1.ObjectRef{
			Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc", Namespace: new("other")},
		}},
		{name: "nil parent reference", owner: "chain/chain-uid", parent: true, ref: &commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef}},
		{name: "nameless parent reference", owner: "chain/chain-uid", parent: true, ref: &commonv1alpha1.ObjectRef{
			Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			chain := testPolicyChain("chain")
			if tt.ref != nil {
				chain.Spec.EventGatewayVirtualClusterRef = *tt.ref
			}

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
				if tt.owner == "chain/chain-uid" && tt.ref == nil {
					require.NotContains(t, parent.Annotations, policyChainOwnerAnnotation)
				} else {
					require.Equal(t, tt.owner, parent.Annotations[policyChainOwnerAnnotation])
				}
			}
		})
	}
}

func TestPolicyChainDeletionRetriesParentErrors(t *testing.T) {
	t.Parallel()
	failure := errors.New("parent unavailable")
	for _, tt := range []struct {
		name     string
		getErr   error
		patchErr error
		requeue  bool
	}{
		{name: "get error", getErr: failure},
		{name: "patch error", patchErr: failure},
		{name: "ownership conflict", patchErr: apierrors.NewConflict(
			schema.GroupResource{Group: configurationv1alpha1.GroupVersion.Group, Resource: "eventgatewayvirtualclusters"},
			"vc", failure,
		), requeue: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			chain := testPolicyChain("chain")
			chain.DeletionTimestamp = new(metav1.Now())
			controllerutil.AddFinalizer(chain, KonnectCleanupFinalizer)
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{
				Name: "vc", Namespace: "default",
				Annotations: map[string]string{policyChainOwnerAnnotation: "chain/chain-uid"},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(chain, parent).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualCluster); ok && tt.getErr != nil {
							return tt.getErr
						}
						return cl.Get(ctx, key, obj, opts...)
					},
					Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualCluster); ok {
							return tt.patchErr
						}
						return cl.Patch(ctx, obj, patch, opts...)
					},
				}).Build()
			res, err := finalizePolicyChain(ctx, cl, chain)
			if tt.requeue {
				require.NoError(t, err)
				require.True(t, res.Requeue)
			} else {
				require.ErrorIs(t, err, failure)
			}
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
			require.Contains(t, chain.Finalizers, KonnectCleanupFinalizer)
		})
	}
}

func TestPolicyChainCrossNamespaceGrantedDeletion(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	chain := testPolicyChain("chain")
	chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Namespace = new("parent")
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{configurationv1alpha1.GroupVersion, konnectv1alpha1.GroupVersion})
	for _, kind := range []string{"EventGatewayVirtualClusterPolicyChain", "EventGatewayVirtualCluster", "EventGatewayBackendCluster", "KongReferenceGrant"} {
		mapper.Add(configurationv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	for _, kind := range []string{"KonnectAPIAuthConfiguration", "KonnectEventGateway"} {
		mapper.Add(konnectv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithRESTMapper(mapper).WithStatusSubresource(
		chain, &configurationv1alpha1.EventGatewayVirtualCluster{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{},
	).WithObjects(chain).Build()
	parentClient := client.NewNamespacedClient(cl, "parent")
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, parentClient)
	gateway := deploy.KonnectEventGateway(t, ctx, parentClient, auth)
	backend := deploy.EventGatewayBackendCluster(t, ctx, parentClient, gateway)
	parent := deploy.EventGatewayVirtualCluster(t, ctx, parentClient, backend, deploy.WithName("vc"))
	parent.SetKonnectID("vc-id")
	parent.SetGatewayID("gateway-id")
	parent.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}}
	require.NoError(t, cl.Status().Update(ctx, parent))
	// Exercise generic grant authorization even though current grant admission
	// does not yet allow the policy-chain kind.
	deploy.KongReferenceGrant(t, ctx, parentClient,
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
			Kind:  "EventGatewayVirtualClusterPolicyChain", Namespace: "default",
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
			Kind:  "EventGatewayVirtualCluster", Name: new(configurationv1alpha1.ObjectName(parent.Name)),
		}),
	)
	factory := sdkmocks.NewMockSDKFactory(t)
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](
		factory, logging.DevelopmentMode, cl,
		WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{}),
	)
	for range 20 {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		chain.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("EventGatewayVirtualClusterPolicyChain"))
		_, err := r.Reconcile(ctx, chain)
		require.NoError(t, err)
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		if apimeta.IsStatusConditionFalse(chain.Status.Conditions, commonv1alpha1.KonnectReferencesResolvedConditionType) {
			break
		}
	}
	require.Contains(t, chain.Finalizers, KonnectCleanupFinalizer)
	require.True(t, apimeta.IsStatusConditionTrue(chain.Status.Conditions, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)))
	refs := apimeta.FindStatusCondition(chain.Status.Conditions, commonv1alpha1.KonnectReferencesResolvedConditionType)
	require.NotNil(t, refs)
	require.Equal(t, commonv1alpha1.KonnectReferencesResolvedReasonInvalid, refs.Reason)
	require.True(t, apimeta.IsStatusConditionFalse(chain.Status.Conditions, "Programmed"))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policyChainOwnerAnnotation)
	require.NoError(t, cl.Delete(ctx, chain))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	_, err := r.Reconcile(ctx, chain)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Empty(t, factory.SDK.EventGatewayVirtualClusterPoliciesSDK.Calls, "no Konnect chain operations during rejection or deletion")
}
