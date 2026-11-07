package konnect

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func testListenerPolicyChain(name string) *configurationv1alpha1.EventGatewayListenerPolicyChain {
	return &configurationv1alpha1.EventGatewayListenerPolicyChain{
		Name: name, Namespace: "default", UID: types.UID(name + "-uid"), Generation: 1,
		Spec: configurationv1alpha1.EventGatewayListenerPolicyChainSpec{
			EventGatewayListenerRef: testNamespacedObjectRef("listener"),
			APISpec: configurationv1alpha1.EventGatewayListenerPolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayListenerPolicyRef{},
			},
		},
	}
}

func TestListenerPolicyChainReconcileSnapshots(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	chain := testListenerPolicyChain("chain")
	chain.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new("")
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "second"}, {Name: "first"}}
	policies := make([]client.Object, 0, 2)
	for _, name := range []string{"first", "second"} {
		policy := &configurationv1alpha1.EventGatewayListenerPolicy{
			Name: name, Namespace: chain.Namespace, Generation: 1,
			Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
				EventGatewayListenerRef: testNamespacedObjectRef("listener"),
			},
		}
		policy.SetKonnectID(name + "-id")
		policy.SetGatewayID("gateway-id")
		policy.SetEventGatewayListenerID("listener-id")
		ops.SetKonnectEntityProgrammedConditionTrue(policy)
		policies = append(policies, policy)
	}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{configurationv1alpha1.GroupVersion, konnectv1alpha1.GroupVersion})
	for _, kind := range []string{"EventGatewayListenerPolicyChain", "EventGatewayListenerPolicy", "EventGatewayListener", "KongReferenceGrant"} {
		mapper.Add(configurationv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	for _, kind := range []string{"KonnectAPIAuthConfiguration", "KonnectEventGateway"} {
		mapper.Add(konnectv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	reads := make(map[string]int)
	cl := newFakeClientBuilder(scheme.Get()).WithRESTMapper(mapper).
		WithStatusSubresource(chain, &configurationv1alpha1.EventGatewayListener{}, &configurationv1alpha1.EventGatewayListenerPolicy{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		WithObjects(append(policies, chain)...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewayListenerPolicy); ok {
					reads[key.Name]++
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}).Build()
	nsClient := client.NewNamespacedClient(cl, chain.Namespace)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, nsClient)
	gateway := deploy.KonnectEventGateway(t, ctx, nsClient, auth)
	parent := &configurationv1alpha1.EventGatewayListener{
		Name: "listener", Namespace: chain.Namespace,
		Spec: configurationv1alpha1.EventGatewayListenerSpec{GatewayRef: testNamespacedObjectRef(gateway.Name)},
	}
	require.NoError(t, cl.Create(ctx, parent))
	parent.SetKonnectID("listener-id")
	parent.SetGatewayID("gateway-id")
	ops.SetKonnectEntityProgrammedConditionTrue(parent)
	require.NoError(t, cl.Status().Update(ctx, parent))

	factory := sdkmocks.NewMockSDKFactory(t)
	remote := []string{"first-id", "second-id"}
	var applied [][]string
	policySDK := factory.SDK.EventGatewayListenerPoliciesSDK
	policySDK.EXPECT().GetEventGatewayListenerPolicyChain(mock.Anything, "gateway-id", "listener-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayListenerPolicyChainResponse, error) {
			return &sdkkonnectops.GetEventGatewayListenerPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	policySDK.EXPECT().UpdateEventGatewayListenerPolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayListenerPolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse, error) {
			remote = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			applied = append(applied, slices.Clone(remote))
			return &sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	r := NewKonnectEntityReconciler(factory, logging.DevelopmentMode, cl,
		WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayListenerPolicyChain](0),
		WithMetricRecorder[configurationv1alpha1.EventGatewayListenerPolicyChain](&metricsmocks.MockRecorder{}),
	)
	reconcile := func() {
		t.Helper()
		reads = make(map[string]int)
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		chain.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("EventGatewayListenerPolicyChain"))
		_, err := r.Reconcile(ctx, chain)
		require.NoError(t, err)
		for name, count := range reads {
			require.Equal(t, 1, count, "policy %s must be resolved once per reconciliation", name)
		}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	}
	reconcileUntilApplied := func(want []string) {
		t.Helper()
		before := len(applied)
		for range 20 {
			reconcile()
			if len(applied) > before {
				require.Equal(t, want, applied[len(applied)-1])
				require.Equal(t, map[string]int{"first": 1, "second": 1}, reads)
				require.True(t, entityHasProgrammedStatusTrue(chain))
				require.Empty(t, chain.GetKonnectID())
				return
			}
		}
		t.Fatalf("reconciliation did not apply the listener chain: conditions=%+v", chain.Status.Conditions)
	}
	reconcileUntilApplied([]string{"second-id", "first-id"})
	remote = []string{"first-id", "second-id"}
	reconcileUntilApplied([]string{"second-id", "first-id"})

	first := policies[0].(*configurationv1alpha1.EventGatewayListenerPolicy)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(first), first))
	first.Generation++
	require.NoError(t, cl.Update(ctx, first))
	before := len(applied)
	reconcile()
	require.Len(t, applied, before)
	require.True(t, apimeta.IsStatusConditionFalse(chain.Status.Conditions, commonv1alpha1.KonnectReferencesResolvedConditionType))
	require.False(t, entityHasProgrammedStatusTrue(chain))
	first.SetKonnectID("recreated-first-id")
	ops.SetKonnectEntityProgrammedConditionTrue(first)
	require.NoError(t, cl.Status().Update(ctx, first))
	remote = []string{"recreated-first-id", "second-id"}
	reconcileUntilApplied([]string{"second-id", "recreated-first-id"})
}

func TestListenerPolicyChainOwnershipAndRetention(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	first, second := testListenerPolicyChain("first"), testListenerPolicyChain("second")
	policy := &configurationv1alpha1.EventGatewayListenerPolicy{Name: "retained-policy", Namespace: first.Namespace}
	parent := &configurationv1alpha1.EventGatewayListener{
		Name: "listener", Namespace: first.Namespace,
		Annotations: map[string]string{policyChainOwnerAnnotation: "independent-cluster-claim"},
	}
	cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(first, second).
		WithObjects(parent, policy, first, second).Build()
	owned, res, err := claimPolicyChain(ctx, cl, first, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.True(t, owned)
	require.True(t, res.IsZero())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policyChainOwnerClaim(first), parent.Annotations[listenerPolicyChainOwnerAnnotation])
	require.Equal(t, "independent-cluster-claim", parent.Annotations[policyChainOwnerAnnotation])

	owned, res, err = claimPolicyChain(ctx, cl, second, 7*time.Second)
	require.NoError(t, err)
	require.False(t, owned)
	require.Equal(t, 7*time.Second, res.RequeueAfter)
	condition := apimeta.FindStatusCondition(second.Status.Conditions, "PolicyChainOwnerValid")
	require.NotNil(t, condition)
	require.Equal(t, "ConflictingChain", condition.Reason)
	require.Contains(t, condition.Message, "EventGatewayListener listener")
	require.False(t, entityHasProgrammedStatusTrue(second))

	controllerutil.AddFinalizer(first, KonnectCleanupFinalizer)
	first.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "missing-policy"}}
	require.NoError(t, cl.Update(ctx, first))
	require.NoError(t, cl.Delete(ctx, first))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(first), first))
	owned, _, err = claimPolicyChain(ctx, cl, second, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.False(t, owned, "terminating owner must retain ownership until cleanup")
	// No SDK factory, auth configuration, parent readiness, or policy references.
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayListenerPolicyChain](nil, logging.DevelopmentMode, cl)
	res, err = r.Reconcile(ctx, first)
	require.NoError(t, err)
	require.True(t, res.IsZero())
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(first), first)))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, listenerPolicyChainOwnerAnnotation)
	require.Equal(t, "independent-cluster-claim", parent.Annotations[policyChainOwnerAnnotation])
	owned, _, err = claimPolicyChain(ctx, cl, second, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.True(t, owned)
}

func TestListenerPolicyChainStaleOwnership(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, claim string
		wrongParent bool
	}{
		{name: "missing owner", claim: "missing/uid"},
		{name: "recreated owner", claim: "other/previous-uid"},
		{name: "malformed", claim: "garbage"},
		{name: "invalid name", claim: "invalid name/uid"},
		{name: "extra separator", claim: "other/other-uid/invalid"},
		{name: "wrong parent", claim: "other/other-uid", wrongParent: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain, other := testListenerPolicyChain("chain"), testListenerPolicyChain("other")
			if tt.wrongParent {
				other.Spec.EventGatewayListenerRef = testNamespacedObjectRef("other-listener")
			}
			parent := &configurationv1alpha1.EventGatewayListener{
				Name: "listener", Namespace: chain.Namespace,
				Annotations: map[string]string{listenerPolicyChainOwnerAnnotation: tt.claim},
			}
			cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(parent, chain, other).Build()
			owned, res, err := claimPolicyChain(t.Context(), cl, chain, consts.DefaultKonnectSyncPeriod)
			require.NoError(t, err)
			require.True(t, owned)
			require.True(t, res.IsZero())
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			require.Equal(t, policyChainOwnerClaim(chain), parent.Annotations[listenerPolicyChainOwnerAnnotation])
		})
	}
}

func TestListenerPolicyChainOwnershipPatchConflict(t *testing.T) {
	t.Parallel()
	chain := testListenerPolicyChain("chain")
	parent := &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: chain.Namespace}
	conflict := apierrors.NewConflict(schema.GroupResource{Group: configurationv1alpha1.GroupVersion.Group, Resource: "eventgatewaylisteners"}, parent.Name, errors.New("stale parent"))
	cl := newFakeClientBuilder(scheme.Get()).WithObjects(chain, parent).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
				require.IsType(t, parent, obj)
				data, err := patch.Data(obj)
				require.NoError(t, err)
				require.Contains(t, string(data), `"resourceVersion"`)
				return conflict
			},
		}).Build()
	owned, res, err := claimPolicyChain(t.Context(), cl, chain, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.False(t, owned)
	require.True(t, res.Requeue)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, listenerPolicyChainOwnerAnnotation)
}

func TestListenerPolicyChainInvalidReferencesBlockProgramming(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		change  func(*configurationv1alpha1.EventGatewayListenerPolicyChain, *configurationv1alpha1.EventGatewayListenerPolicy)
		wantErr string
	}{
		{name: "missing name", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ *configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Name = ""
		}, wantErr: "must name"},
		{name: "wrong kind", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ *configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Kind = "EventGatewayVirtualClusterPolicy"
		}, wantErr: "must name"},
		{name: "cross namespace", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ *configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Namespace = "other"
		}, wantErr: "cross-namespace"},
		{name: "duplicate", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ *configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies = append(c.Spec.APISpec.Policies, c.Spec.APISpec.Policies[0])
		}, wantErr: "duplicates"},
		{name: "missing policy", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain, _ *configurationv1alpha1.EventGatewayListenerPolicy) {
			c.Spec.APISpec.Policies[0].Name = "missing"
		}, wantErr: "not found"},
		{name: "different listener", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.Spec.EventGatewayListenerRef = testNamespacedObjectRef("other")
		}, wantErr: "different EventGatewayListener"},
		{name: "different listener ID", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.SetEventGatewayListenerID("other")
		}, wantErr: "different EventGatewayListener"},
		{name: "different gateway", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.SetGatewayID("other")
		}, wantErr: "not referrer Gateway"},
		{name: "deleting", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.DeletionTimestamp = new(metav1.Now())
			p.Finalizers = []string{KonnectCleanupFinalizer}
		}, wantErr: "being deleted"},
		{name: "no ID", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.SetKonnectID("")
		}, wantErr: "not programmed"},
		{name: "stale generation", change: func(_ *configurationv1alpha1.EventGatewayListenerPolicyChain, p *configurationv1alpha1.EventGatewayListenerPolicy) {
			p.Generation++
		}, wantErr: "not programmed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := testListenerPolicyChain("chain")
			chain.SetGatewayID("gateway-id")
			chain.SetEventGatewayListenerID("listener-id")
			chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "policy"}}
			policy := &configurationv1alpha1.EventGatewayListenerPolicy{
				Name: "policy", Namespace: chain.Namespace, Generation: 1,
				Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{EventGatewayListenerRef: testNamespacedObjectRef("listener")},
			}
			policy.SetKonnectID("policy-id")
			policy.SetGatewayID("gateway-id")
			policy.SetEventGatewayListenerID("listener-id")
			ops.SetKonnectEntityProgrammedConditionTrue(policy)
			tt.change(chain, policy)
			cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(chain, policy).Build()
			ids, err := chain.ResolveValidatedPolicyIDs(t.Context(), cl)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, ids, "no shortened result may escape validation")
			_, stop, res, err := handleTypeSpecific(t.Context(), nil, consts.DefaultKonnectSyncPeriod, cl, cl, chain)
			require.NoError(t, err)
			require.True(t, stop)
			require.True(t, res.IsZero())
			condition := apimeta.FindStatusCondition(chain.Status.Conditions, commonv1alpha1.KonnectReferencesResolvedConditionType)
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			require.Contains(t, condition.Message, tt.wantErr)
			require.False(t, entityHasProgrammedStatusTrue(chain))
		})
	}
}

func TestListenerPolicyChainOwnershipUsesLiveReads(t *testing.T) {
	t.Parallel()
	chain, owner := testListenerPolicyChain("chain"), testListenerPolicyChain("owner")
	parent := &configurationv1alpha1.EventGatewayListener{
		Name: "listener", Namespace: chain.Namespace,
		Annotations: map[string]string{listenerPolicyChainOwnerAnnotation: policyChainOwnerClaim(owner)},
	}
	cached := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(chain, parent).Build()
	live := newFakeClientBuilder(scheme.Get()).WithObjects(chain, owner, parent).Build()
	owned, res, err := claimPolicyChain(t.Context(), policyChainLiveGetClient{Client: cached, apiReader: live}, chain, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.False(t, owned, "an owner missing only from the cache is not stale")
	require.Equal(t, consts.DefaultKonnectSyncPeriod, res.RequeueAfter)
	require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policyChainOwnerClaim(owner), parent.Annotations[listenerPolicyChainOwnerAnnotation])
}

func TestListenerPolicyChainStaleSelfCannotClaim(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(*configurationv1alpha1.EventGatewayListenerPolicyChain)
	}{
		{name: "recreated", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain) { c.UID = "new-uid" }},
		{name: "updated declaration", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain) { c.Generation++ }},
		{name: "terminating", change: func(c *configurationv1alpha1.EventGatewayListenerPolicyChain) {
			c.DeletionTimestamp = new(metav1.Now())
			c.Finalizers = []string{KonnectCleanupFinalizer}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := testListenerPolicyChain("chain")
			current := chain.DeepCopy()
			tt.change(current)
			parent := &configurationv1alpha1.EventGatewayListener{Name: "listener", Namespace: chain.Namespace}
			cached := newFakeClientBuilder(scheme.Get()).WithObjects(chain, parent).Build()
			live := newFakeClientBuilder(scheme.Get()).WithObjects(current, parent).Build()
			owned, res, err := claimPolicyChain(t.Context(), policyChainLiveGetClient{Client: cached, apiReader: live}, chain, consts.DefaultKonnectSyncPeriod)
			require.NoError(t, err)
			require.False(t, owned)
			require.True(t, res.Requeue)
			require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			require.NotContains(t, parent.Annotations, listenerPolicyChainOwnerAnnotation)
		})
	}
}

func TestListenerPolicyChainOwnershipReadErrors(t *testing.T) {
	t.Parallel()
	for _, failingName := range []string{"chain", "listener", "owner"} {
		t.Run(failingName, func(t *testing.T) {
			chain, owner := testListenerPolicyChain("chain"), testListenerPolicyChain("owner")
			parent := &configurationv1alpha1.EventGatewayListener{
				Name: "listener", Namespace: chain.Namespace,
				Annotations: map[string]string{listenerPolicyChainOwnerAnnotation: policyChainOwnerClaim(owner)},
			}
			readErr := errors.New("live reader unavailable")
			cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(chain, owner, parent).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key.Name == failingName {
							return readErr
						}
						return cl.Get(ctx, key, obj, opts...)
					},
				}).Build()
			owned, res, err := claimPolicyChain(t.Context(), cl, chain, consts.DefaultKonnectSyncPeriod)
			require.ErrorIs(t, err, readErr)
			require.False(t, owned)
			require.True(t, res.IsZero())
			require.Equal(t, policyChainOwnerClaim(owner), parent.Annotations[listenerPolicyChainOwnerAnnotation])
		})
	}
}

func TestListenerPolicyChainDeletionKeepsDifferentUIDClaim(t *testing.T) {
	t.Parallel()
	chain := testListenerPolicyChain("chain")
	chain.Finalizers = []string{KonnectCleanupFinalizer}
	parent := &configurationv1alpha1.EventGatewayListener{
		Name: "listener", Namespace: chain.Namespace,
		Annotations: map[string]string{listenerPolicyChainOwnerAnnotation: chain.Name + "/another-uid"},
	}
	cl := newFakeClientBuilder(scheme.Get()).WithObjects(chain, parent).Build()
	require.NoError(t, cl.Delete(t.Context(), chain))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain))
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayListenerPolicyChain](nil, logging.DevelopmentMode, cl)
	res, err := r.Reconcile(t.Context(), chain)
	require.NoError(t, err)
	require.True(t, res.IsZero())
	require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain)))
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, chain.Name+"/another-uid", parent.Annotations[listenerPolicyChainOwnerAnnotation])
}
