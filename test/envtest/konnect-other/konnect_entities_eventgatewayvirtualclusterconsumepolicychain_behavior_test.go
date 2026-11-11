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

func TestEventGatewayVirtualClusterConsumePolicyChainConcurrentOwnership(t *testing.T) {
	t.Parallel()
	var claims *consumeChainClaimsClient
	ctx, cl, vc, remote := startConsumeChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		claims = &consumeChainClaimsClient{Client: cl, ready: make(chan struct{})}
		return claims
	})
	for _, name := range []string{"first", "second", "root"} {
		programConsumeChainPolicy(t, ctx, cl, createConsumeChainPolicy(t, ctx, cl, vc, name, ""), name+"-id")
	}
	remote.setPolicies("first-id", "second-id", "root-id")
	produce, cluster := newProduceChain(vc, "produce"), newPolicyChain(vc, "cluster")
	cluster.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
	require.NoError(t, cl.Create(ctx, produce))
	require.NoError(t, cl.Create(ctx, cluster))
	waitForProduceChainCondition(t, cl, produce, "Programmed", metav1.ConditionTrue, "Programmed")
	waitForChainCondition(t, cl, cluster, "Programmed", metav1.ConditionTrue, "Programmed")
	chains := []*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{
		newConsumeChain(vc, "forward", "root", "first", "second"), newConsumeChain(vc, "reverse", "second", "first", "root"),
	}
	for _, chain := range chains {
		require.NoError(t, cl.Create(ctx, chain))
	}
	require.Eventually(t, func() bool { return claims.conflicts.Load() > 0 }, consts.WaitTime, consts.TickTime,
		"both claimants must race against the real apiserver resourceVersion")
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	owner, loser := chains[0], chains[1]
	if vc.Annotations[policychain.ConsumeOwnerAnnotation] == policychain.ClaimValue(loser) {
		owner, loser = loser, owner
	}
	waitForConsumeChainCondition(t, cl, owner, "Programmed", metav1.ConditionTrue, "Programmed")
	waitForConsumeChainCondition(t, cl, loser, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	writes := remote.writes()
	require.NotEmpty(t, writes)
	for _, write := range writes {
		require.Equal(t, policychain.ClaimValue(owner), write.owner, "losing claimant must never PUT")
	}
	assertNoChainWrites(t, remote, len(writes))
	require.NoError(t, cl.Delete(ctx, owner))
	waitForConsumeChainCondition(t, cl, loser, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Eventually(t, func() bool { return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(owner), owner)) },
		consts.WaitTime, consts.TickTime)
	for _, write := range remote.writes()[len(writes):] {
		require.Equal(t, policychain.ClaimValue(loser), write.owner)
	}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(loser), vc.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(produce), vc.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), vc.Annotations[policychain.OwnerAnnotation])
}

func TestEventGatewayVirtualClusterConsumePolicyChainLiveOwnerCacheLag(t *testing.T) {
	t.Parallel()
	var lagged *consumeChainClaimsClient
	ctx, cl, vc, remote := startConsumeChainBehaviorTest(t, time.Second, func(cl client.Client) client.Client {
		lagged = &consumeChainClaimsClient{Client: cl, hideOwner: true}
		return lagged
	})
	owner := newConsumeChain(vc, "owner")
	require.NoError(t, cl.Create(ctx, owner))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	old := vc.DeepCopy()
	vc.Annotations = map[string]string{policychain.ConsumeOwnerAnnotation: policychain.ClaimValue(owner)}
	require.NoError(t, cl.Patch(ctx, vc, client.MergeFrom(old)))
	contender := newConsumeChain(vc, "contender")
	require.NoError(t, cl.Create(ctx, contender))
	waitForConsumeChainCondition(t, cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	require.Eventually(t, func() bool { return lagged.misses.Load() > 0 }, consts.WaitTime, consts.TickTime)
	assertNoChainWrites(t, remote, 0)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(owner), vc.Annotations[policychain.ConsumeOwnerAnnotation])
	require.NoError(t, cl.Delete(ctx, owner))
	waitForConsumeChainCondition(t, cl, contender, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(contender), vc.Annotations[policychain.ConsumeOwnerAnnotation])
}

func TestEventGatewayVirtualClusterConsumePolicyChainEmptyParentAndStaleClaims(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startConsumeChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, nil)
	chain := newConsumeChain(vc, "empty")
	vc.Status.Conditions[0].Status = metav1.ConditionFalse
	require.NoError(t, cl.Status().Update(ctx, vc))
	require.NoError(t, cl.Create(ctx, chain))
	waitForConsumeChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "NotProgrammed")
	assertNoChainWrites(t, remote, 0)
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	for _, stale := range []string{"malformed", "missing/uid", chain.Name + "/previous-uid"} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
		old := vc.DeepCopy()
		vc.Annotations[policychain.ConsumeOwnerAnnotation] = stale
		require.NoError(t, cl.Patch(ctx, vc, client.MergeFrom(old)))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc)) {
				assert.Equal(ct, policychain.ClaimValue(chain), vc.Annotations[policychain.ConsumeOwnerAnnotation])
			}
		}, consts.WaitTime, consts.TickTime)
	}
	oldUID := vc.UID
	before := len(remote.writes())
	require.NoError(t, cl.Delete(ctx, vc))
	waitForConsumeChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "Invalid")
	assertNoChainWrites(t, remote, before)
	recreated := &configurationv1alpha1.EventGatewayVirtualCluster{Name: vc.Name, Namespace: vc.Namespace, Spec: *vc.Spec.DeepCopy()}
	require.NoError(t, cl.Create(ctx, recreated))
	require.NotEqual(t, oldUID, recreated.UID)
	waitForConsumeChainCondition(t, cl, chain, "EventGatewayVirtualClusterRefValid", metav1.ConditionFalse, "NotProgrammed")
	recreated.SetKonnectID("recreated-vc-id")
	recreated.SetGatewayID("gateway-id")
	recreated.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(recreated.Generation)}
	require.NoError(t, cl.Status().Update(ctx, recreated))
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	require.Equal(t, "recreated-vc-id", chain.GetVirtualClusterID())
	require.Empty(t, chain.GetKonnectID())
	assertNoChainWrites(t, remote, before)
}

func TestEventGatewayVirtualClusterConsumePolicyChainNestedDriftAndRealRecreation(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startConsumeChainBehaviorTest(t, time.Second, nil)
	parent := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, vc.Namespace, `
metadata: {name: parent}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    type: schemaValidation
    schemaValidation: {name: parent, config: {type: json, json: {valueValidationAction: skip}}}
`)
	require.NoError(t, cl.Create(ctx, parent))
	first := createConsumeChainPolicy(t, ctx, cl, vc, "first", "parent")
	second := createConsumeChainPolicy(t, ctx, cl, vc, "second", "parent")
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{parent, first, second} {
		programConsumeChainPolicy(t, ctx, cl, p, p.Name+"-id")
	}
	remote.setPolicies("parent-id", "first-id", "second-id")
	chain := newConsumeChain(vc, "chain", "parent", "second", "first")
	require.NoError(t, cl.Create(ctx, chain))
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	before := len(remote.writes())
	remote.setPolicies("parent-id", "first-id", "second-id")
	require.Eventually(t, func() bool { return len(remote.writes()) > before }, consts.WaitTime, consts.TickTime)
	assertChainWrites(t, remote, before, []string{"parent-id", "second-id", "first-id"})
	before = len(remote.writes())
	// A stale ancestry status must block rather than leak a shortened or foreign chain.
	first.SetVirtualClusterID("foreign-vc-id")
	require.NoError(t, cl.Status().Update(ctx, first))
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceInvalid")
	assertNoChainWrites(t, remote, before)
	programConsumeChainPolicy(t, ctx, cl, first, "first-id")
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	originalUID := first.UID
	require.NoError(t, cl.Delete(ctx, first))
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	remote.setPolicies("parent-id", "second-id")
	first = createConsumeChainPolicy(t, ctx, cl, vc, "first", "parent")
	require.NotEqual(t, originalUID, first.UID)
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, before)
	remote.setPolicies("parent-id", "first-recreated-id", "second-id")
	programConsumeChainPolicy(t, ctx, cl, first, "first-recreated-id")
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, []string{"parent-id", "second-id", "first-recreated-id"})
	require.Equal(t, "parent", first.Spec.APISpec.ParentPolicyRef.Name)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, "parent-id", parent.GetKonnectID())
}

type consumeChainClaimsClient struct {
	client.Client

	ready     chan struct{}
	hideOwner bool
	attempts  atomic.Int32
	conflicts atomic.Int32
	misses    atomic.Int32
}

func (c *consumeChainClaimsClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	parent, ok := obj.(*configurationv1alpha1.EventGatewayVirtualCluster)
	claiming := ok && parent.Annotations[policychain.ConsumeOwnerAnnotation] != ""
	if claiming && c.ready != nil {
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

func (c *consumeChainClaimsClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain); ok && c.hideOwner && key.Name == "owner" {
		c.misses.Add(1)
		return apierrors.NewNotFound(schema.GroupResource{Group: configurationv1alpha1.GroupVersion.Group, Resource: "eventgatewayvirtualclusterconsumepolicychains"}, key.Name)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func startConsumeChainBehaviorTest(t *testing.T, period time.Duration, wrap func(client.Client) client.Client) (context.Context, client.Client, *configurationv1alpha1.EventGatewayVirtualCluster, *policyChainRemote) {
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
	vcKey := client.ObjectKeyFromObject(vc)
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, vcID string, _ ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, vcKey, &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, parent.GetKonnectID(), vcID)
			remote.mu.Lock()
			defer remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		}).Maybe()
	sdk.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, vcKey, &parent); err != nil {
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
			remote.requests = append(remote.requests, policyChainWrite{owner: parent.Annotations[policychain.ConsumeOwnerAnnotation], policies: slices.Clone(requested)})
			remote.policies = slices.Clone(requested)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(requested)},
			}, nil
		}).Maybe()
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
		Return(&sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
		Return(&sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil).Maybe()
	reconcilerClient := mgr.GetClient()
	if wrap != nil {
		reconcilerClient = wrap(reconcilerClient)
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, reconcilerClient,
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](period),
			konnect.WithControllerOptions[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](controller.Options{MaxConcurrentReconciles: 2}),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](&metricsmocks.MockRecorder{})),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](&metricsmocks.MockRecorder{})),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{})),
	)
	t.Cleanup(cancel)
	return ctx, cl, vc, remote
}

func newConsumeChain(vc *configurationv1alpha1.EventGatewayVirtualCluster, name string, names ...string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain {
	chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{
		Name: name, Namespace: vc.Namespace,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainSpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name}},
			APISpec:                       configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainAPISpec{Policies: []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{}},
		},
	}
	for _, name := range names {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: name})
	}
	return chain
}

func createConsumeChainPolicy(t *testing.T, ctx context.Context, cl client.Client, vc *configurationv1alpha1.EventGatewayVirtualCluster, name, parent string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	t.Helper()
	p := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](t, vc.Namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: `+vc.Name+`}}
  apiSpec:
    type: modifyHeaders
    modifyHeaders: {name: `+name+`, config: {actions: [{op: set, set: {key: x-chain, value: example}}]}}
`)
	if parent != "" {
		p.Spec.APISpec.ParentPolicyRef.Name = parent
	}
	require.NoError(t, cl.Create(ctx, p))
	return p
}

func programConsumeChainPolicy(t *testing.T, ctx context.Context, cl client.Client, p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy, id string) {
	t.Helper()
	p.SetKonnectID(id)
	p.SetGatewayID("gateway-id")
	p.SetVirtualClusterID("vc-id")
	p.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(p.Generation)}
	require.NoError(t, cl.Status().Update(ctx, p))
}
