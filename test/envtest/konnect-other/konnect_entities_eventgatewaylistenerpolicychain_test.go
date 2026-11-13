package konnectother

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const listenerChainOwnerAnnotation = "gateway-operator.konghq.com/listener-policy-chain-owner"

func TestEventGatewayListenerPolicyChainReferenceWatchRecovery(t *testing.T) {
	t.Parallel()
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, nil)
	first := f.createPolicy(t, ctx, f.listener, "first")
	f.programPolicy(t, ctx, first, "first-id")
	f.remote.setPolicies("first-id", "second-id")
	chain := newListenerChain(f.listener, "chain", "second", "first")
	require.NoError(t, f.cl.Create(ctx, chain))
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	waitForListenerChainCondition(t, f.cl, chain, "Programmed", metav1.ConditionFalse, "")
	assertNoChainWrites(t, f.remote, 0)

	second := f.createPolicy(t, ctx, f.listener, "second")
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, f.remote, 0)
	f.programPolicy(t, ctx, second, "second-id")
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, 0, []string{"second-id", "first-id"})

	before := len(f.remote.writes())
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(first), first))
	first.Spec.APISpec.ForwardToVirtualClust.Description = "new unprogrammed generation"
	require.NoError(t, f.cl.Update(ctx, first))
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, f.remote, before)
	f.programPolicy(t, ctx, first, "first-id")
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, before, []string{"second-id", "first-id"})

	foreignParent := f.listener.DeepCopy()
	foreignParent.Name = "other-listener"
	foreign := f.createPolicy(t, ctx, foreignParent, "foreign")
	f.programPolicy(t, ctx, foreign, "foreign-id")
	for _, invalid := range []struct {
		name, message, reason string
		policies              []configurationv1alpha1.EventGatewayListenerPolicyRef
	}{
		{name: "wrong listener", message: "different EventGatewayListener", reason: "ReferenceInvalid", policies: []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "foreign"}, {Name: "first"}}},
		{name: "duplicate", message: "duplicates", reason: "ReferenceInvalid", policies: []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "first"}, {Name: "first", Namespace: f.listener.Namespace}}},
		{name: "cross namespace policy without grant", message: "not granted by any KongReferenceGrant", reason: "ReferenceNotPermitted", policies: []configurationv1alpha1.EventGatewayListenerPolicyRef{{Name: "second", Namespace: "other"}, {Name: "first"}}},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			before := len(f.remote.writes())
			setListenerChainReferences(t, ctx, f.cl, chain, invalid.policies)
			waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, invalid.reason)
			waitForListenerChainCondition(t, f.cl, chain, "Programmed", metav1.ConditionFalse, "")
			require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "KonnectReferencesResolved").Message, invalid.message)
			assertNoChainWrites(t, f.remote, before)
			setListenerChainPolicies(t, ctx, f.cl, chain, "second", "first")
			waitForListenerChainProgrammed(t, f.cl, chain)
			assertChainWrites(t, f.remote, before, []string{"second-id", "first-id"})
		})
	}
}

func TestEventGatewayListenerPolicyChainParentWatchAndEmptyScope(t *testing.T) {
	t.Parallel()
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, nil)
	f.listener.Status.Conditions[0].Status = metav1.ConditionFalse
	require.NoError(t, f.cl.Status().Update(ctx, f.listener))
	chain := newListenerChain(f.listener, "empty")
	require.NotNil(t, chain.Spec.APISpec.Policies)
	require.NoError(t, f.cl.Create(ctx, chain))
	waitForListenerChainCondition(t, f.cl, chain, "EventGatewayListenerRefValid", metav1.ConditionFalse, "NotProgrammed")
	assertNoChainWrites(t, f.remote, 0)
	f.listener.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(f.listener.Generation)}
	require.NoError(t, f.cl.Status().Update(ctx, f.listener))
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, 0, []string{})

	oldUID := f.listener.UID
	before := len(f.remote.writes())
	require.NoError(t, f.cl.Delete(ctx, f.listener))
	waitForListenerChainCondition(t, f.cl, chain, "EventGatewayListenerRefValid", metav1.ConditionFalse, "Invalid")
	assertNoChainWrites(t, f.remote, before)
	recreated := &configurationv1alpha1.EventGatewayListener{
		Name: f.listener.Name, Namespace: f.listener.Namespace,
		Spec: *f.listener.Spec.DeepCopy(),
	}
	require.NoError(t, f.cl.Create(ctx, recreated))
	require.NotEqual(t, oldUID, recreated.UID)
	waitForListenerChainCondition(t, f.cl, chain, "EventGatewayListenerRefValid", metav1.ConditionFalse, "NotProgrammed")
	assertNoChainWrites(t, f.remote, before)
	recreated.SetKonnectID("recreated-listener-id")
	recreated.SetGatewayID("gateway-id")
	recreated.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(recreated.Generation)}
	require.NoError(t, f.cl.Status().Update(ctx, recreated))
	waitForListenerChainProgrammed(t, f.cl, chain)
	require.Equal(t, "recreated-listener-id", chain.GetEventGatewayListenerID())
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(recreated), recreated))
	require.Equal(t, listenerChainClaim(chain), recreated.Annotations[listenerChainOwnerAnnotation])
	assertChainWrites(t, f.remote, before, []string{})
}

func TestEventGatewayListenerPolicyChainPolicySetDriftAndRecreation(t *testing.T) {
	t.Parallel()
	ctx, f := startListenerChainTest(t, time.Second, nil)
	first := f.createPolicy(t, ctx, f.listener, "first")
	f.programPolicy(t, ctx, first, "first-id")
	second := f.createPolicy(t, ctx, f.listener, "second")
	f.programPolicy(t, ctx, second, "second-id")
	f.remote.setPolicies("first-id", "second-id")
	chain := newListenerChain(f.listener, "chain", "second", "first")
	require.NoError(t, f.cl.Create(ctx, chain))
	waitForListenerChainProgrammed(t, f.cl, chain)

	before := len(f.remote.writes())
	f.remote.setPolicies("first-id", "second-id")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.Greater(ct, len(f.remote.writes()), before)
		f.remote.mu.Lock()
		defer f.remote.mu.Unlock()
		assert.Equal(ct, []string{"second-id", "first-id"}, f.remote.policies)
	}, consts.WaitTime, consts.TickTime)
	assertChainWrites(t, f.remote, before, []string{"second-id", "first-id"})

	f.remote.setPolicies("first-id", "second-id", "third-id")
	waitForListenerChainCondition(t, f.cl, chain, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "Programmed").Message, "complete remote listener policy chain")
	before = len(f.remote.writes())
	third := f.createPolicy(t, ctx, f.listener, "third")
	f.programPolicy(t, ctx, third, "third-id")
	assertNoChainWrites(t, f.remote, before)
	setListenerChainPolicies(t, ctx, f.cl, chain, "third", "second", "first")
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, before, []string{"third-id", "second-id", "first-id"})

	oldUID := first.UID
	require.NoError(t, f.cl.Delete(ctx, first))
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	before = len(f.remote.writes())
	f.remote.setPolicies("third-id", "second-id")
	assertNoChainWrites(t, f.remote, before)
	first = f.createPolicy(t, ctx, f.listener, "first")
	require.NotEqual(t, oldUID, first.UID)
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, f.remote, before)
	f.remote.setPolicies("second-id", "first-recreated-id", "third-id")
	f.programPolicy(t, ctx, first, "first-recreated-id")
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, before, []string{"third-id", "second-id", "first-recreated-id"})

	require.NoError(t, f.cl.Delete(ctx, third))
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	before = len(f.remote.writes())
	f.remote.setPolicies("second-id", "first-recreated-id")
	assertNoChainWrites(t, f.remote, before)
	setListenerChainPolicies(t, ctx, f.cl, chain, "first", "second")
	waitForListenerChainProgrammed(t, f.cl, chain)
	assertChainWrites(t, f.remote, before, []string{"first-recreated-id", "second-id"})
}

func TestEventGatewayListenerPolicyChainConcurrentOwnership(t *testing.T) {
	t.Parallel()
	var claims *concurrentListenerChainClaims
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		claims = &concurrentListenerChainClaims{Client: cl, ready: make(chan struct{})}
		return claims
	})
	for _, name := range []string{"first", "second"} {
		policy := f.createPolicy(t, ctx, f.listener, name)
		f.programPolicy(t, ctx, policy, name+"-id")
	}
	f.remote.setPolicies("first-id", "second-id")
	chains := []*configurationv1alpha1.EventGatewayListenerPolicyChain{
		newListenerChain(f.listener, "forward", "first", "second"),
		newListenerChain(f.listener, "reverse", "second", "first"),
	}
	for _, chain := range chains {
		require.NoError(t, f.cl.Create(ctx, chain))
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.GreaterOrEqual(ct, claims.conflicts.Load(), int32(1), "the API server must reject a stale ownership patch")
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	owner, contender := chains[0], chains[1]
	if f.listener.Annotations[listenerChainOwnerAnnotation] == listenerChainClaim(contender) {
		owner, contender = contender, owner
	}
	require.Equal(t, listenerChainClaim(owner), f.listener.Annotations[listenerChainOwnerAnnotation])
	waitForListenerChainProgrammed(t, f.cl, owner)
	waitForListenerChainCondition(t, f.cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	waitForListenerChainCondition(t, f.cl, contender, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(contender.Status.Conditions, "PolicyChainOwnerValid").Message, owner.Name)
	writes := f.remote.writes()
	require.NotEmpty(t, writes)
	for _, write := range writes {
		require.Equal(t, listenerChainClaim(owner), write.owner)
		require.Equal(t, listenerChainIDs(owner), write.policies, "the contender must not PUT")
	}
	assertNoChainWrites(t, f.remote, len(writes))

	require.NoError(t, f.cl.Delete(ctx, owner))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(f.cl.Get(ctx, client.ObjectKeyFromObject(owner), owner)))
	}, consts.WaitTime, consts.TickTime)
	waitForListenerChainProgrammed(t, f.cl, contender)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	require.Equal(t, listenerChainClaim(contender), f.listener.Annotations[listenerChainOwnerAnnotation])
	after := f.remote.writes()
	require.Greater(t, len(after), len(writes))
	for _, write := range after[len(writes):] {
		require.Equal(t, listenerChainClaim(contender), write.owner)
		require.Equal(t, listenerChainIDs(contender), write.policies)
	}
}

func TestEventGatewayListenerPolicyChainLiveOwnerSafety(t *testing.T) {
	t.Parallel()
	var lagged *laggedListenerChainClient
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		lagged = &laggedListenerChainClient{Client: cl}
		return lagged
	})
	owner := newListenerChain(f.listener, "owner")
	require.NoError(t, f.cl.Create(ctx, owner))
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	old := f.listener.DeepCopy()
	f.listener.Annotations = map[string]string{listenerChainOwnerAnnotation: listenerChainClaim(owner)}
	require.NoError(t, f.cl.Patch(ctx, f.listener, client.MergeFrom(old)))
	contender := newListenerChain(f.listener, "contender")
	require.NoError(t, f.cl.Create(ctx, contender))
	waitForListenerChainCondition(t, f.cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	waitForListenerChainCondition(t, f.cl, contender, "Programmed", metav1.ConditionFalse, "")
	require.Eventually(t, func() bool { return lagged.misses.Load() > 0 }, consts.WaitTime, consts.TickTime)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	require.Equal(t, listenerChainClaim(owner), f.listener.Annotations[listenerChainOwnerAnnotation])
	assertNoChainWrites(t, f.remote, 0)
}

func TestEventGatewayListenerPolicyChainDeletionWithoutAuthOrReferences(t *testing.T) {
	t.Parallel()
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, nil)
	var policies []*configurationv1alpha1.EventGatewayListenerPolicy
	for _, name := range []string{"first", "second"} {
		policy := f.createPolicy(t, ctx, f.listener, name)
		f.programPolicy(t, ctx, policy, name+"-id")
		policies = append(policies, policy.DeepCopy())
	}
	f.remote.setPolicies("first-id", "second-id")
	chain := newListenerChain(f.listener, "chain", "second", "first")
	require.NoError(t, f.cl.Create(ctx, chain))
	waitForListenerChainProgrammed(t, f.cl, chain)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	old := f.listener.DeepCopy()
	f.listener.Annotations[chainOwnerAnnotation] = "independent-cluster-owner"
	require.NoError(t, f.cl.Patch(ctx, f.listener, client.MergeFrom(old)))
	setListenerChainPolicies(t, ctx, f.cl, chain, "missing")
	waitForListenerChainCondition(t, f.cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	before := len(f.remote.writes())
	require.NoError(t, f.cl.Delete(ctx, f.auth))
	require.NoError(t, f.cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(f.cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	require.NotContains(t, f.listener.Annotations, listenerChainOwnerAnnotation)
	require.Equal(t, "independent-cluster-owner", f.listener.Annotations[chainOwnerAnnotation])
	for _, original := range policies {
		var current configurationv1alpha1.EventGatewayListenerPolicy
		require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(original), &current))
		require.Equal(t, original.UID, current.UID)
		require.Equal(t, original.GetKonnectID(), current.GetKonnectID())
		require.True(t, current.DeletionTimestamp.IsZero())
	}
	assertNoChainWrites(t, f.remote, before)
	f.remote.mu.Lock()
	defer f.remote.mu.Unlock()
	require.Equal(t, []string{"second-id", "first-id"}, f.remote.policies)
}

func TestEventGatewayListenerPolicyChainCrossNamespaceDeletion(t *testing.T) {
	t.Parallel()
	ctx, f := startListenerChainTest(t, consts.KonnectInfiniteSyncTime, nil)
	ns := deploy.Namespace(t, ctx, f.cl)
	chain := newListenerChain(f.listener, "cross-namespace")
	chain.Namespace = ns.Name
	chain.Spec.EventGatewayListenerRef.NamespacedRef.Namespace = new(f.listener.Namespace)
	chain.Finalizers = []string{konnect.KonnectCleanupFinalizer}
	require.NoError(t, f.cl.Create(ctx, chain))
	waitForListenerChainCondition(t, f.cl, chain, "ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
	waitForListenerChainCondition(t, f.cl, chain, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, chain.Finalizers, konnect.KonnectCleanupFinalizer)
	require.Nil(t, meta.FindStatusCondition(chain.Status.Conditions, chain.GetStatusConditionTypeParentRefValid()))
	require.Empty(t, chain.GetEventGatewayListenerID())
	require.Empty(t, chain.GetGatewayID())
	require.NoError(t, f.cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(f.cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, f.cl.Get(ctx, client.ObjectKeyFromObject(f.listener), f.listener))
	require.NotContains(t, f.listener.Annotations, listenerChainOwnerAnnotation)
	assertNoChainWrites(t, f.remote, 0)
}

type concurrentListenerChainClaims struct {
	client.Client

	ready     chan struct{}
	attempts  atomic.Int32
	conflicts atomic.Int32
}

func (c *concurrentListenerChainClaims) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	parent, isParent := obj.(*configurationv1alpha1.EventGatewayListener)
	claiming := isParent && parent.Annotations[listenerChainOwnerAnnotation] != ""
	if claiming {
		switch c.attempts.Add(1) {
		case 1:
			select {
			case <-c.ready:
			case <-ctx.Done():
				return ctx.Err()
			}
		case 2:
			close(c.ready)
		}
	}
	err := c.Client.Patch(ctx, obj, patch, opts...)
	if claiming && apierrors.IsConflict(err) {
		c.conflicts.Add(1)
	}
	return err
}

type laggedListenerChainClient struct {
	client.Client

	misses atomic.Int32
}

func (c *laggedListenerChainClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, isChain := obj.(*configurationv1alpha1.EventGatewayListenerPolicyChain); isChain && key.Name == "owner" {
		c.misses.Add(1)
		return apierrors.NewNotFound(schema.GroupResource{Group: configurationv1alpha1.GroupVersion.Group, Resource: "eventgatewaylistenerpolicychains"}, key.Name)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type listenerChainFixture struct {
	cl       client.Client
	listener *configurationv1alpha1.EventGatewayListener
	auth     *konnectv1alpha1.KonnectAPIAuthConfiguration
	remote   *policyChainRemote
}

func startListenerChainTest(t *testing.T, syncPeriod time.Duration, wrapClient func(client.Client) client.Client) (context.Context, *listenerChainFixture) {
	t.Helper()
	ctx, cancel := envtest.Context(t, t.Context())
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get(), envtest.WithManagerOptions(func(o *manager.Options) {
		o.Cache.SyncPeriod = new(consts.KonnectInfiniteSyncTime)
	}))
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	nsClient := client.NewNamespacedClient(cl, ns.Name)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, nsClient)
	gateway := deploy.KonnectEventGateway(t, ctx, nsClient, auth)
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, nsClient, gateway, "gateway-id")
	listener := objectFromYAML[configurationv1alpha1.EventGatewayListener](t, ns.Name, `
metadata: {name: listener}
spec:
  gatewayRef: {type: namespacedRef, namespacedRef: {name: `+gateway.Name+`}}
  apiSpec: {name: listener, addresses: ["0.0.0.0"], ports: ["19092"]}
`)
	require.NoError(t, cl.Create(ctx, listener))
	listener.SetKonnectID("listener-id")
	listener.SetGatewayID("gateway-id")
	listener.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(listener.Generation)}
	require.NoError(t, cl.Status().Update(ctx, listener))
	f := &listenerChainFixture{cl: cl, listener: listener, auth: auth, remote: &policyChainRemote{policies: []string{}}}
	parentKey := client.ObjectKeyFromObject(listener)
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayListenerPoliciesSDK
	sdk.EXPECT().GetEventGatewayListenerPolicyChain(mock.Anything, "gateway-id", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, listenerID string, _ ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayListenerPolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayListener
			if err := cl.Get(ctx, parentKey, &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, parent.GetKonnectID(), listenerID)
			f.remote.mu.Lock()
			defer f.remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayListenerPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(f.remote.policies)},
			}, nil
		}).Maybe()
	sdk.EXPECT().UpdateEventGatewayListenerPolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayListenerPolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayListener
			if err := cl.Get(ctx, parentKey, &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, "gateway-id", req.GatewayID)
			assert.Equal(t, parent.GetKonnectID(), req.ListenerID)
			if !assert.NotNil(t, req.UpdateEventGatewayPolicyChainRequest) {
				return nil, assert.AnError
			}
			f.remote.mu.Lock()
			defer f.remote.mu.Unlock()
			f.remote.requests = append(f.remote.requests, policyChainWrite{
				owner: parent.Annotations[listenerChainOwnerAnnotation], policies: slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies),
			})
			current, requested := slices.Clone(f.remote.policies), slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			slices.Sort(current)
			slices.Sort(requested)
			if !slices.Equal(current, requested) {
				return nil, fmt.Errorf("remote listener policy set changed between GET and PUT: %v != %v", current, requested)
			}
			f.remote.policies = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			return &sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(f.remote.policies)},
			}, nil
		}).Maybe()
	reconcilerClient := mgr.GetClient()
	if wrapClient != nil {
		reconcilerClient = wrapClient(reconcilerClient)
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, reconcilerClient,
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayListenerPolicyChain](syncPeriod),
			konnect.WithControllerOptions[configurationv1alpha1.EventGatewayListenerPolicyChain](controller.Options{MaxConcurrentReconciles: 2}),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayListenerPolicyChain](&metricsmocks.MockRecorder{}),
		),
	)
	t.Cleanup(cancel)
	return ctx, f
}

func (f *listenerChainFixture) createPolicy(t *testing.T, ctx context.Context, parent *configurationv1alpha1.EventGatewayListener, name string) *configurationv1alpha1.EventGatewayListenerPolicy {
	t.Helper()
	policy := objectFromYAML[configurationv1alpha1.EventGatewayListenerPolicy](t, parent.Namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: `+parent.Name+`}}
  apiSpec:
    type: forwardToVirtualCluster
    forwardToVirtualCluster:
      name: `+name+`
      enabled: Enabled
      config:
        type: portMapping
        portMapping:
          bootstrapPort: at_start
          advertisedHost: placeholder.invalid
          destination: {name: virtual-cluster}
`)
	require.NoError(t, f.cl.Create(ctx, policy))
	policy.SetGatewayID("gateway-id")
	policy.SetEventGatewayListenerID(parent.GetKonnectID())
	require.NoError(t, f.cl.Status().Update(ctx, policy))
	return policy
}

func (f *listenerChainFixture) programPolicy(t *testing.T, ctx context.Context, policy *configurationv1alpha1.EventGatewayListenerPolicy, id string) {
	t.Helper()
	policy.SetKonnectID(id)
	policy.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(policy.Generation)}
	require.NoError(t, f.cl.Status().Update(ctx, policy))
}

func newListenerChain(listener *configurationv1alpha1.EventGatewayListener, name string, names ...string) *configurationv1alpha1.EventGatewayListenerPolicyChain {
	chain := &configurationv1alpha1.EventGatewayListenerPolicyChain{
		Name: name, Namespace: listener.Namespace,
		Spec: configurationv1alpha1.EventGatewayListenerPolicyChainSpec{
			EventGatewayListenerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: listener.Name},
			},
			APISpec: configurationv1alpha1.EventGatewayListenerPolicyChainAPISpec{Policies: []configurationv1alpha1.EventGatewayListenerPolicyRef{}},
		},
	}
	for _, name := range names {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayListenerPolicyRef{Name: name})
	}
	return chain
}

func setListenerChainPolicies(t *testing.T, ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayListenerPolicyChain, names ...string) {
	t.Helper()
	refs := make([]configurationv1alpha1.EventGatewayListenerPolicyRef, 0, len(names))
	for _, name := range names {
		refs = append(refs, configurationv1alpha1.EventGatewayListenerPolicyRef{Name: name})
	}
	setListenerChainReferences(t, ctx, cl, chain, refs)
}

func setListenerChainReferences(t *testing.T, ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayListenerPolicyChain, refs []configurationv1alpha1.EventGatewayListenerPolicyRef) {
	t.Helper()
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	old := chain.DeepCopy()
	chain.Spec.APISpec.Policies = refs
	require.NoError(t, cl.Patch(ctx, chain, client.MergeFrom(old)))
}

func waitForListenerChainCondition(t *testing.T, cl client.Client, chain *configurationv1alpha1.EventGatewayListenerPolicyChain, conditionType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain)) {
			return
		}
		c := meta.FindStatusCondition(chain.Status.Conditions, conditionType)
		if assert.NotNil(ct, c) {
			assert.Equal(ct, status, c.Status, "%s", c.Message)
			assert.Equal(ct, chain.Generation, c.ObservedGeneration, "%s", c.Message)
			if reason != "" {
				assert.Equal(ct, reason, c.Reason, "%s", c.Message)
			}
		}
	}, consts.WaitTime, consts.TickTime)
}

func waitForListenerChainProgrammed(t *testing.T, cl client.Client, chain *configurationv1alpha1.EventGatewayListenerPolicyChain) {
	t.Helper()
	for _, condition := range []struct{ name, reason string }{
		{"EventGatewayListenerRefValid", "Valid"},
		{"APIAuthResolvedRef", "ResolvedRef"},
		{"APIAuthValid", "Valid"},
		{"KonnectReferencesResolved", "Resolved"},
		{"PolicyChainOwnerValid", "Owner"},
		{"Programmed", "Programmed"},
	} {
		waitForListenerChainCondition(t, cl, chain, condition.name, metav1.ConditionTrue, condition.reason)
	}
	require.Empty(t, chain.GetKonnectID())
	require.False(t, chain.PersistsKonnectID())
}

func listenerChainClaim(chain *configurationv1alpha1.EventGatewayListenerPolicyChain) string {
	return chain.Name + "/" + string(chain.UID)
}

func listenerChainIDs(chain *configurationv1alpha1.EventGatewayListenerPolicyChain) []string {
	ids := make([]string, 0, len(chain.Spec.APISpec.Policies))
	for _, ref := range chain.Spec.APISpec.Policies {
		ids = append(ids, ref.Name+"-id")
	}
	return ids
}
