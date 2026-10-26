package konnectother

import (
	"context"
	"slices"
	"sync"
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

func TestEventGatewayVirtualClusterPolicyChainLifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](time.Second),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{}),
		),
	)
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	nsClient := client.NewNamespacedClient(cl, ns.Name)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, nsClient)
	gateway := deploy.KonnectEventGateway(t, ctx, nsClient, auth)
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, nsClient, gateway, "gateway-id")
	backend := deploy.EventGatewayBackendCluster(t, ctx, nsClient, gateway)
	backend.SetKonnectID("backend-id")
	backend.SetGatewayID("gateway-id")
	backend.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(backend.Generation)}
	require.NoError(t, cl.Status().Update(ctx, backend))
	vc := deploy.EventGatewayVirtualCluster(t, ctx, nsClient, backend, deploy.WithName("vc"))
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))

	var policies []*configurationv1alpha1.EventGatewayVirtualClusterPolicy
	for _, name := range []string{"first", "second"} {
		policy := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicy](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
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
		policy.SetKonnectID(name + "-id")
		policy.SetGatewayID("gateway-id")
		policy.SetVirtualClusterID("vc-id")
		policy.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(policy.Generation)}
		require.NoError(t, cl.Status().Update(ctx, policy))
		policies = append(policies, policy)
	}

	var mu sync.Mutex
	remote := []string{"first-id", "second-id"}
	policySDK := factory.SDK.EventGatewayVirtualClusterPoliciesSDK
	policySDK.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	policySDK.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything,
		mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest) bool {
			return req.GatewayID == "gateway-id" && req.VirtualClusterID == "vc-id" && req.UpdateEventGatewayPolicyChainRequest != nil
		})).
		RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse, error) {
			mu.Lock()
			defer mu.Unlock()
			remote = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	chain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: chain}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    policies: [{name: second}, {name: first}]
`)
	require.NoError(t, cl.Create(ctx, chain))
	waitForPolicyCondition(t, cl, chain, "Programmed", "Programmed")
	require.Empty(t, chain.GetKonnectID())

	waitForOrder := func(expected []string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(ct, expected, remote)
		}, consts.WaitTime, consts.TickTime)
	}
	waitForOrder([]string{"second-id", "first-id"})
	mu.Lock()
	remote = []string{"first-id", "second-id"}
	mu.Unlock()
	waitForOrder([]string{"second-id", "first-id"})

	t.Log("A recreated policy is not applied until its new generation is programmed")
	first := policies[0]
	first.Spec.APISpec.ACLs.Description = "recreated"
	require.NoError(t, cl.Update(ctx, first))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)) {
			return
		}
		c := meta.FindStatusCondition(chain.Status.Conditions, "KonnectReferencesResolved")
		if assert.NotNil(ct, c) {
			assert.Equal(ct, metav1.ConditionFalse, c.Status)
			assert.Equal(ct, "ReferenceNotProgrammed", c.Reason)
		}
	}, consts.WaitTime, consts.TickTime)
	mu.Lock()
	remote = []string{"first-recreated-id", "second-id"}
	mu.Unlock()
	first.SetKonnectID("first-recreated-id")
	first.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(first.Generation)}
	require.NoError(t, cl.Status().Update(ctx, first))
	waitForOrder([]string{"second-id", "first-recreated-id"})

	t.Log("Deletion retains configuration even without authentication")
	require.NoError(t, cl.Delete(ctx, auth))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	require.NoError(t, cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.NotContains(t, vc.Annotations, "gateway-operator.konghq.com/cluster-policy-chain-owner")
	for _, policy := range policies {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	}
	mu.Lock()
	require.Equal(t, []string{"second-id", "first-recreated-id"}, remote)
	mu.Unlock()
}

func TestEventGatewayVirtualClusterPolicyChainCrossNamespaceDeletion(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](
			factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{}),
		),
	)
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	parentNS := deploy.Namespace(t, ctx, cl)
	parentClient := client.NewNamespacedClient(cl, parentNS.Name)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, parentClient)
	gateway := deploy.KonnectEventGateway(t, ctx, parentClient, auth)
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, parentClient, gateway, "gateway-id")
	backend := deploy.EventGatewayBackendCluster(t, ctx, parentClient, gateway)
	vc := deploy.EventGatewayVirtualCluster(t, ctx, parentClient, backend, deploy.WithName("vc"))
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	chain := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](t, ns.Name, `
metadata: {name: cross-namespace}
spec:
  eventGatewayVirtualClusterRef:
    type: namespacedRef
    namespacedRef: {name: vc, namespace: `+parentNS.Name+`}
  apiSpec: {policies: []}
`)
	// KongReferenceGrant admission currently excludes this kind. Seed the cleanup
	// finalizer here; the grant-authorized runtime path is covered by a fake-client test.
	chain.Finalizers = []string{konnect.KonnectCleanupFinalizer}
	require.NoError(t, cl.Create(ctx, chain), "cross-namespace parents are admitted")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)) {
			return
		}
		assert.Contains(ct, chain.Finalizers, konnect.KonnectCleanupFinalizer)
		grant := meta.FindStatusCondition(chain.Status.Conditions, "ResolvedRefs")
		if assert.NotNil(ct, grant) {
			assert.Equal(ct, metav1.ConditionFalse, grant.Status)
			assert.Equal(ct, string(configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted), grant.Reason)
		}
		programmed := meta.FindStatusCondition(chain.Status.Conditions, "Programmed")
		if assert.NotNil(ct, programmed) {
			assert.Equal(ct, metav1.ConditionFalse, programmed.Status)
		}
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.NotContains(t, vc.Annotations, "gateway-operator.konghq.com/cluster-policy-chain-owner")
	require.NoError(t, cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.AssertNotCalled(t, "GetEventGatewayVirtualClusterClusterLevelPolicyChain", mock.Anything, mock.Anything, mock.Anything)
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.AssertNotCalled(t, "UpdateEventGatewayVirtualClusterClusterLevelPolicyChain", mock.Anything, mock.Anything)
}
