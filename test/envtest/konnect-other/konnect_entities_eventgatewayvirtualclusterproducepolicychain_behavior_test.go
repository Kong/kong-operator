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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayVirtualClusterProducePolicyChainConcurrentOwnership(t *testing.T) {
	t.Parallel()
	var claims *concurrentProduceChainClaims
	ctx, cl, vc, remote := startProduceChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		claims = &concurrentProduceChainClaims{Client: cl, ready: make(chan struct{})}
		return claims
	})
	for _, name := range []string{"first", "second"} {
		programProduceChainPolicy(t, ctx, cl, createProduceChainPolicy(t, ctx, cl, vc, name), name+"-id")
	}
	remote.setPolicies("first-id", "second-id")
	// Both singleton scopes must be able to own the very same virtual cluster.
	clusterChain := newPolicyChain(vc, "cluster")
	clusterChain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
	require.NoError(t, cl.Create(ctx, clusterChain))
	waitForChainCondition(t, cl, clusterChain, "Programmed", metav1.ConditionTrue, "Programmed")
	chains := []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
		newProduceChain(vc, "forward", "first", "second"), newProduceChain(vc, "reverse", "second", "first"),
	}
	for _, chain := range chains {
		require.NoError(t, cl.Create(ctx, chain))
	}
	require.Eventually(t, func() bool { return claims.conflicts.Load() > 0 }, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	owner, loser := chains[0], chains[1]
	if vc.Annotations[policychain.ProduceOwnerAnnotation] == policychain.ClaimValue(loser) {
		owner, loser = loser, owner
	}
	require.Equal(t, policychain.ClaimValue(owner), vc.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(clusterChain), vc.Annotations[policychain.OwnerAnnotation])
	waitForProduceChainCondition(t, cl, owner, "Programmed", metav1.ConditionTrue, "Programmed")
	waitForProduceChainCondition(t, cl, loser, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	writes := remote.writes()
	require.NotEmpty(t, writes)
	for _, write := range writes {
		require.Equal(t, policychain.ClaimValue(owner), write.owner, "the losing claimant must never PUT")
	}
	assertNoChainWrites(t, remote, len(writes))
	require.NoError(t, cl.Delete(ctx, owner))
	waitForProduceChainCondition(t, cl, loser, "Programmed", metav1.ConditionTrue, "Programmed")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(owner), owner)))
	}, consts.WaitTime, consts.TickTime)
	for _, write := range remote.writes()[len(writes):] {
		require.Equal(t, policychain.ClaimValue(loser), write.owner)
	}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(clusterChain), vc.Annotations[policychain.OwnerAnnotation])
	waitForChainCondition(t, cl, clusterChain, "Programmed", metav1.ConditionTrue, "Programmed")
}

func TestEventGatewayVirtualClusterProducePolicyChainLiveOwnerSafety(t *testing.T) {
	t.Parallel()
	var lagged *laggedProduceChainClient
	ctx, cl, vc, remote := startProduceChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		lagged = &laggedProduceChainClient{Client: cl}
		return lagged
	})
	owner := newProduceChain(vc, "owner")
	require.NoError(t, cl.Create(ctx, owner))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	old := vc.DeepCopy()
	vc.Annotations = map[string]string{policychain.ProduceOwnerAnnotation: policychain.ClaimValue(owner)}
	require.NoError(t, cl.Patch(ctx, vc, client.MergeFrom(old)))
	contender := newProduceChain(vc, "contender")
	require.NoError(t, cl.Create(ctx, contender))
	waitForProduceChainCondition(t, cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	require.Eventually(t, func() bool { return lagged.misses.Load() > 0 }, consts.WaitTime, consts.TickTime)
	assertNoChainWrites(t, remote, 0)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(owner), vc.Annotations[policychain.ProduceOwnerAnnotation])
}

func TestEventGatewayVirtualClusterProducePolicyChainEmptyParentRecreation(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startProduceChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, nil)
	chain := newProduceChain(vc, "empty")
	vc.Status.Conditions[0].Status = metav1.ConditionFalse
	require.NoError(t, cl.Status().Update(ctx, vc))
	require.NoError(t, cl.Create(ctx, chain))
	waitForProduceChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "NotProgrammed")
	assertNoChainWrites(t, remote, 0)
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	for _, stale := range []string{"malformed", "missing/uid", chain.Name + "/previous-uid"} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
		old := vc.DeepCopy()
		vc.Annotations[policychain.ProduceOwnerAnnotation] = stale
		require.NoError(t, cl.Patch(ctx, vc, client.MergeFrom(old)))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc)) {
				assert.Equal(ct, policychain.ClaimValue(chain), vc.Annotations[policychain.ProduceOwnerAnnotation])
			}
		}, consts.WaitTime, consts.TickTime)
	}
	oldUID := vc.UID
	before := len(remote.writes())
	require.NoError(t, cl.Delete(ctx, vc))
	waitForProduceChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "Invalid")
	assertNoChainWrites(t, remote, before)
	recreated := &configurationv1alpha1.EventGatewayVirtualCluster{Name: vc.Name, Namespace: vc.Namespace, Spec: *vc.Spec.DeepCopy()}
	require.NoError(t, cl.Create(ctx, recreated))
	require.NotEqual(t, oldUID, recreated.UID)
	waitForProduceChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "NotProgrammed")
	recreated.SetKonnectID("recreated-vc-id")
	recreated.SetGatewayID("gateway-id")
	recreated.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(recreated.Generation)}
	require.NoError(t, cl.Status().Update(ctx, recreated))
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Equal(t, "recreated-vc-id", chain.GetVirtualClusterID())
	assertChainWrites(t, remote, before, []string{})
}

func TestEventGatewayVirtualClusterProducePolicyChainDriftAndRealRecreation(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startProduceChainBehaviorTest(t, time.Second, nil)
	first := createProduceChainPolicy(t, ctx, cl, vc, "first")
	second := createProduceChainPolicy(t, ctx, cl, vc, "second")
	programProduceChainPolicy(t, ctx, cl, first, "first-id")
	programProduceChainPolicy(t, ctx, cl, second, "second-id")
	remote.setPolicies("first-id", "second-id")
	chain := newProduceChain(vc, "chain", "second", "first")
	require.NoError(t, cl.Create(ctx, chain))
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	before := len(remote.writes())
	remote.setPolicies("first-id", "second-id")
	require.Eventually(t, func() bool { return len(remote.writes()) > before }, consts.WaitTime, consts.TickTime)
	assertChainWrites(t, remote, before, []string{"second-id", "first-id"})
	originalUID := first.UID
	require.NoError(t, cl.Delete(ctx, first))
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	remote.setPolicies("second-id")
	before = len(remote.writes())
	first = createProduceChainPolicy(t, ctx, cl, vc, "first")
	require.NotEqual(t, originalUID, first.UID)
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, before)
	remote.setPolicies("first-recreated-id", "second-id")
	programProduceChainPolicy(t, ctx, cl, first, "first-recreated-id")
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, []string{"second-id", "first-recreated-id"})
	require.Empty(t, chain.GetKonnectID())
}

type concurrentProduceChainClaims struct {
	client.Client

	ready     chan struct{}
	attempts  atomic.Int32
	conflicts atomic.Int32
}

func (c *concurrentProduceChainClaims) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	parent, ok := obj.(*configurationv1alpha1.EventGatewayVirtualCluster)
	claiming := ok && parent.Annotations[policychain.ProduceOwnerAnnotation] != ""
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

type laggedProduceChainClient struct {
	client.Client

	misses atomic.Int32
}

func (c *laggedProduceChainClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain); ok && key.Name == "owner" {
		c.misses.Add(1)
		return apierrors.NewNotFound(schema.GroupResource{Group: configurationv1alpha1.GroupVersion.Group, Resource: "eventgatewayvirtualclusterproducepolicychains"}, key.Name)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func startProduceChainBehaviorTest(t *testing.T, period time.Duration, wrap func(client.Client) client.Client) (context.Context, client.Client, *configurationv1alpha1.EventGatewayVirtualCluster, *policyChainRemote) {
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
	backend := deploy.EventGatewayBackendCluster(t, ctx, nsClient, gateway)
	vc := deploy.EventGatewayVirtualCluster(t, ctx, nsClient, backend, deploy.WithName("vc"))
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	remote := &policyChainRemote{policies: []string{}}
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, vcID string, _ ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, client.ObjectKeyFromObject(vc), &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, parent.GetKonnectID(), vcID)
			remote.mu.Lock()
			defer remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		}).Maybe()
	sdk.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, client.ObjectKeyFromObject(vc), &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, parent.GetKonnectID(), req.VirtualClusterID)
			remote.mu.Lock()
			defer remote.mu.Unlock()
			requested := req.UpdateEventGatewayPolicyChainRequest.Policies
			current, desired := slices.Clone(remote.policies), slices.Clone(requested)
			slices.Sort(current)
			slices.Sort(desired)
			if !slices.Equal(current, desired) {
				return nil, fmt.Errorf("policy set changed between GET and PUT")
			}
			remote.requests = append(remote.requests, policyChainWrite{owner: parent.Annotations[policychain.ProduceOwnerAnnotation], policies: slices.Clone(requested)})
			remote.policies = slices.Clone(requested)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(requested)},
			}, nil
		}).Maybe()
	clusterSDK := factory.SDK.EventGatewayVirtualClusterPoliciesSDK
	clusterSDK.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
		Return(&sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	clusterSDK.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	reconcilerClient := mgr.GetClient()
	if wrap != nil {
		reconcilerClient = wrap(reconcilerClient)
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, reconcilerClient,
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](period),
			konnect.WithControllerOptions[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](controller.Options{MaxConcurrentReconciles: 2}),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](&metricsmocks.MockRecorder{})),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{})),
	)
	t.Cleanup(cancel)
	return ctx, cl, vc, remote
}

func newProduceChain(vc *configurationv1alpha1.EventGatewayVirtualCluster, name string, names ...string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain {
	chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
		Name: name, Namespace: vc.Namespace,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name}},
			APISpec:                       configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{Policies: []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{}},
		},
	}
	for _, name := range names {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: name})
	}
	return chain
}

func createProduceChainPolicy(t *testing.T, ctx context.Context, cl client.Client, vc *configurationv1alpha1.EventGatewayVirtualCluster, name string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
	t.Helper()
	p := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, vc.Namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: `+vc.Name+`}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders: {name: `+name+`, config: {actions: [{op: set, set: {key: x-chain, value: example}}]}}
`)
	require.NoError(t, cl.Create(ctx, p))
	return p
}

func programProduceChainPolicy(t *testing.T, ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, id string) {
	t.Helper()
	p.SetKonnectID(id)
	p.SetGatewayID("gateway-id")
	p.SetVirtualClusterID("vc-id")
	p.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(p.Generation)}
	require.NoError(t, cl.Status().Update(ctx, p))
}
