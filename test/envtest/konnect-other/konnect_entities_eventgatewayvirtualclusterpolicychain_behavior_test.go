package konnectother

import (
	"context"
	"fmt"
	"slices"
	"sync"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const chainOwnerAnnotation = "gateway-operator.konghq.com/cluster-policy-chain-owner"

func TestEventGatewayVirtualClusterPolicyChainConcurrentOwnership(t *testing.T) {
	t.Parallel()
	var claims *concurrentPolicyChainClaims
	ctx, cl, vc, remote := startPolicyChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, func(cl client.Client) client.Client {
		claims = &concurrentPolicyChainClaims{Client: cl, ready: make(chan struct{})}
		return claims
	})
	for _, name := range []string{"first", "second"} {
		policy := createChainPolicy(t, ctx, cl, vc, name)
		programChainPolicy(t, ctx, cl, policy, name+"-id")
	}
	remote.setPolicies("first-id", "second-id")
	chains := []*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
		newPolicyChain(vc, "forward", "first", "second"),
		newPolicyChain(vc, "reverse", "second", "first"),
	}
	for _, chain := range chains {
		require.NoError(t, cl.Create(ctx, chain))
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.GreaterOrEqual(ct, claims.conflicts.Load(), int32(1), "both claimants must patch the same resourceVersion")
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	owner, contender := chains[0], chains[1]
	if vc.Annotations[chainOwnerAnnotation] == chainClaim(contender) {
		owner, contender = contender, owner
	}
	require.Equal(t, chainClaim(owner), vc.Annotations[chainOwnerAnnotation])
	waitForChainCondition(t, cl, owner, "Programmed", metav1.ConditionTrue, "Programmed")
	waitForChainCondition(t, cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	waitForChainCondition(t, cl, contender, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(contender.Status.Conditions, "PolicyChainOwnerValid").Message, owner.Name)

	ownerOrder := chainPolicyIDs(owner)
	writes := remote.writes()
	require.NotEmpty(t, writes)
	for _, write := range writes {
		require.Equal(t, chainClaim(owner), write.owner)
		require.Equal(t, ownerOrder, write.policies, "the conflicting chain must not PUT")
	}
	assertNoChainWrites(t, remote, len(writes))

	require.NoError(t, cl.Delete(ctx, owner))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(owner), owner)))
	}, consts.WaitTime, consts.TickTime)
	waitForChainCondition(t, cl, contender, "PolicyChainOwnerValid", metav1.ConditionTrue, "Owner")
	waitForChainCondition(t, cl, contender, "Programmed", metav1.ConditionTrue, "Programmed")
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, chainClaim(contender), vc.Annotations[chainOwnerAnnotation])
	afterTakeover := remote.writes()
	require.Greater(t, len(afterTakeover), len(writes))
	for _, write := range afterTakeover[len(writes):] {
		require.Equal(t, chainClaim(contender), write.owner)
		require.Equal(t, chainPolicyIDs(contender), write.policies)
	}
}

func TestEventGatewayVirtualClusterPolicyChainReferenceWatchRecovery(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startPolicyChainBehaviorTest(t, consts.KonnectInfiniteSyncTime, nil)
	first := createChainPolicy(t, ctx, cl, vc, "first")
	programChainPolicy(t, ctx, cl, first, "first-id")
	remote.setPolicies("first-id", "second-id")
	chain := newPolicyChain(vc, "chain", "second", "first")
	require.NoError(t, cl.Create(ctx, chain))
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
	assertNoChainWrites(t, remote, 0)

	second := createChainPolicy(t, ctx, cl, vc, "second")
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, 0)
	// Neither cache resync nor the Konnect sync timer can rescue a missing watch.
	programChainPolicy(t, ctx, cl, second, "second-id")
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionTrue, "Resolved")
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, 0, []string{"second-id", "first-id"})

	foreignParent := vc.DeepCopy()
	foreignParent.Name = "other-vc"
	foreign := createChainPolicy(t, ctx, cl, foreignParent, "foreign")
	programChainPolicy(t, ctx, cl, foreign, "foreign-id")
	for _, invalid := range []struct {
		name     string
		policies []string
		message  string
	}{
		{name: "wrong parent", policies: []string{"foreign", "first"}, message: "different"},
		{name: "duplicate", policies: []string{"first", "first"}, message: "duplicates"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			before := len(remote.writes())
			setChainPolicies(t, ctx, cl, chain, invalid.policies...)
			waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceInvalid")
			waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
			require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "KonnectReferencesResolved").Message, invalid.message)
			assertNoChainWrites(t, remote, before)
			setChainPolicies(t, ctx, cl, chain, "second", "first")
			waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
			assertChainWrites(t, remote, before, []string{"second-id", "first-id"})
		})
	}
}

func TestEventGatewayVirtualClusterPolicyChainPolicySetAndRecreation(t *testing.T) {
	t.Parallel()
	ctx, cl, vc, remote := startPolicyChainBehaviorTest(t, time.Second, nil)
	first := createChainPolicy(t, ctx, cl, vc, "first")
	programChainPolicy(t, ctx, cl, first, "first-id")
	second := createChainPolicy(t, ctx, cl, vc, "second")
	programChainPolicy(t, ctx, cl, second, "second-id")
	remote.setPolicies("first-id", "second-id")
	chain := newPolicyChain(vc, "chain", "second", "first")
	require.NoError(t, cl.Create(ctx, chain))
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")

	// Change Konnect first: no policy watch can race ahead of remote creation.
	remote.setPolicies("first-id", "second-id", "third-id")
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "Programmed").Message, "complete remote cluster policy chain")
	before := len(remote.writes())
	third := createChainPolicy(t, ctx, cl, vc, "third")
	programChainPolicy(t, ctx, cl, third, "third-id")
	assertNoChainWrites(t, remote, before)
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
	setChainPolicies(t, ctx, cl, chain, "third", "second", "first")
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, []string{"third-id", "second-id", "first-id"})

	oldUID := first.UID
	require.NoError(t, cl.Delete(ctx, first))
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	before = len(remote.writes())
	remote.setPolicies("third-id", "second-id")
	assertNoChainWrites(t, remote, before)
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(first), first)))
	first = createChainPolicy(t, ctx, cl, vc, "first")
	require.NotEqual(t, oldUID, first.UID, "this must be a real Kubernetes delete/create, not a status update")
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, before)
	remote.setPolicies("second-id", "first-recreated-id", "third-id")
	programChainPolicy(t, ctx, cl, first, "first-recreated-id")
	require.NotEqual(t, "first-id", first.GetKonnectID())
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, []string{"third-id", "second-id", "first-recreated-id"})

	require.NoError(t, cl.Delete(ctx, third))
	waitForChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	before = len(remote.writes())
	remote.setPolicies("second-id", "first-recreated-id")
	assertNoChainWrites(t, remote, before)
	setChainPolicies(t, ctx, cl, chain, "first", "second")
	waitForChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, []string{"first-recreated-id", "second-id"})
}

// Hold the first two ownership patches until both workers have read the unowned
// parent, then let the API server (not a fake client) resolve their stale writes.
type concurrentPolicyChainClaims struct {
	client.Client

	ready     chan struct{}
	attempts  atomic.Int32
	conflicts atomic.Int32
}

func (c *concurrentPolicyChainClaims) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	parent, isParent := obj.(*configurationv1alpha1.EventGatewayVirtualCluster)
	claiming := isParent && parent.Annotations[chainOwnerAnnotation] != ""
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

type policyChainWrite struct {
	owner    string
	policies []string
}

type policyChainRemote struct {
	mu       sync.Mutex
	policies []string
	requests []policyChainWrite
}

func (r *policyChainRemote) setPolicies(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies = slices.Clone(ids)
}

func (r *policyChainRemote) writes() []policyChainWrite {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.requests)
}

func startPolicyChainBehaviorTest(
	t *testing.T,
	syncPeriod time.Duration,
	wrapClient func(client.Client) client.Client,
) (context.Context, client.Client, *configurationv1alpha1.EventGatewayVirtualCluster, *policyChainRemote) {
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

	remote := &policyChainRemote{}
	parentKey := client.ObjectKeyFromObject(vc)
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayVirtualClusterPoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse, error) {
			remote.mu.Lock()
			defer remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		})
	sdk.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, parentKey, &parent); err != nil {
				return nil, err
			}
			assert.Equal(t, "gateway-id", req.GatewayID)
			assert.Equal(t, "vc-id", req.VirtualClusterID)
			if !assert.NotNil(t, req.UpdateEventGatewayPolicyChainRequest) {
				return nil, assert.AnError
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			remote.requests = append(remote.requests, policyChainWrite{
				owner: parent.Annotations[chainOwnerAnnotation], policies: slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies),
			})
			current, requested := slices.Clone(remote.policies), slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			slices.Sort(current)
			slices.Sort(requested)
			if !slices.Equal(current, requested) {
				return nil, fmt.Errorf("remote policy set changed between GET and PUT: %v != %v", current, requested)
			}
			remote.policies = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		})
	reconcilerClient := mgr.GetClient()
	if wrapClient != nil {
		reconcilerClient = wrapClient(reconcilerClient)
	}
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, reconcilerClient,
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](syncPeriod),
			konnect.WithControllerOptions[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](controller.Options{MaxConcurrentReconciles: 2}),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{}),
		),
	)
	t.Cleanup(cancel)
	return ctx, cl, vc, remote
}

func createChainPolicy(t *testing.T, ctx context.Context, cl client.Client, vc *configurationv1alpha1.EventGatewayVirtualCluster, name string) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
	t.Helper()
	policy := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, vc.Namespace, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: `+vc.Name+`}}
  apiSpec:
    type: acls
    acls:
      config:
        rules:
          - action: allow
            operations: [{name: read}]
            resourceNames: {type: stat, stat: [{match: "*"}]}
            resourceType: topic
`)
	require.NoError(t, cl.Create(ctx, policy))
	policy.SetGatewayID("gateway-id")
	policy.SetVirtualClusterID("vc-id")
	require.NoError(t, cl.Status().Update(ctx, policy))
	return policy
}

func programChainPolicy(t *testing.T, ctx context.Context, cl client.Client, policy *configurationv1alpha1.EventGatewayVirtualClusterPolicy, id string) {
	t.Helper()
	policy.SetKonnectID(id)
	policy.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(policy.Generation)}
	require.NoError(t, cl.Status().Update(ctx, policy))
}

func newPolicyChain(vc *configurationv1alpha1.EventGatewayVirtualCluster, name string, policies ...string) *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain {
	chain := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}
	chain.Name, chain.Namespace = name, vc.Namespace
	chain.Spec.EventGatewayVirtualClusterRef = commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name},
	}
	for _, policy := range policies {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: policy})
	}
	return chain
}

func setChainPolicies(t *testing.T, ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, names ...string) {
	t.Helper()
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	old := chain.DeepCopy()
	chain.Spec.APISpec.Policies = make([]configurationv1alpha1.EventGatewayVirtualClusterPolicyRef, 0, len(names))
	for _, name := range names {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: name})
	}
	require.NoError(t, cl.Patch(ctx, chain, client.MergeFrom(old)))
}

func waitForChainCondition(t *testing.T, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, conditionType string, status metav1.ConditionStatus, reason string) {
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

func assertNoChainWrites(t *testing.T, remote *policyChainRemote, count int) {
	t.Helper()
	require.Never(t, func() bool { return len(remote.writes()) != count }, time.Second, 50*time.Millisecond, "invalid chains must not PUT")
}

func assertChainWrites(t *testing.T, remote *policyChainRemote, start int, expected []string) {
	t.Helper()
	writes := remote.writes()
	require.Greater(t, len(writes), start)
	for _, write := range writes[start:] {
		require.Equal(t, expected, write.policies, "every PUT must carry the exact complete order")
	}
}

func chainClaim(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
	return chain.Name + "/" + string(chain.UID)
}

func chainPolicyIDs(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) []string {
	ids := make([]string, 0, len(chain.Spec.APISpec.Policies))
	for _, ref := range chain.Spec.APISpec.Policies {
		ids = append(ids, ref.Name+"-id")
	}
	return ids
}
