package konnecteventgateway

import (
	"context"
	"fmt"
	"slices"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestEventGatewayVirtualClusterProducePolicyChainNestedRecovery(t *testing.T) {
	t.Parallel()
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
	vc.Annotations = map[string]string{policychain.OwnerAnnotation: "independent-cluster/uid"}
	require.NoError(t, cl.Update(ctx, vc))
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	remote := &policyChainRemote{}
	remote.setPolicies("root-id", "parent-id", "first-id", "second-id")
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			remote.mu.Lock()
			defer remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		})
	sdk.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			var parent configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, client.ObjectKeyFromObject(vc), &parent); err != nil {
				return nil, err
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			requested := req.UpdateEventGatewayPolicyChainRequest.Policies
			current, desired := slices.Clone(remote.policies), slices.Clone(requested)
			slices.Sort(current)
			slices.Sort(desired)
			if !slices.Equal(current, desired) {
				return nil, fmt.Errorf("policy set changed between GET and PUT")
			}
			remote.requests = append(remote.requests, policyChainWrite{
				owner: parent.Annotations[policychain.ProduceOwnerAnnotation], policies: slices.Clone(requested),
			})
			remote.policies = slices.Clone(requested)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(requested)},
			}, nil
		})
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](&metricsmocks.MockRecorder{}),
		),
	)
	t.Cleanup(cancel)

	createPolicy := func(name, parent string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		t.Helper()
		parentField := ""
		if parent != "" {
			parentField = "parentPolicyRef: {name: " + parent + "}"
		}
		apiSpec := "type: modifyHeaders\n    modifyHeaders: {name: " + name + ", config: {actions: [{op: set, set: {key: x-chain, value: example}}]}}"
		if name == "parent" {
			apiSpec = "type: schemaValidation\n    schemaValidation: {name: parent, config: {type: json, json: {valueValidationAction: reject}}}"
		}
		policy := objectFromYAML[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vc}}
  apiSpec:
    `+parentField+`
    `+apiSpec)
		require.NoError(t, cl.Create(ctx, policy))
		return policy
	}
	programPolicy := func(p *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy, id string) {
		t.Helper()
		p.SetKonnectID(id)
		p.SetVirtualClusterID("vc-id")
		p.SetGatewayID("gateway-id")
		p.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(p.Generation)}
		require.NoError(t, cl.Status().Update(ctx, p))
	}
	parent, first, root := createPolicy("parent", ""), createPolicy("first", "parent"), createPolicy("root", "")
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{parent, first, root} {
		programPolicy(p, p.Name+"-id")
	}
	chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{Name: "chain", Namespace: ns.Name,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name}},
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{{Name: "parent"}, {Name: "second"}, {Name: "first"}, {Name: "root"}},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, chain))
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	assertNoChainWrites(t, remote, 0)
	second := createPolicy("second", "parent")
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, 0)
	programPolicy(second, "second-id")
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	want := []string{"parent-id", "second-id", "first-id", "root-id"}
	assertChainWrites(t, remote, 0, want)
	require.Empty(t, chain.GetKonnectID())

	before := len(remote.writes())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	original := parent.Spec.APISpec.DeepCopy()
	parent.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = root.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig.DeepCopy()
	require.NoError(t, cl.Update(ctx, parent))
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	programPolicy(parent, "parent-id")
	waitForProduceChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceInvalid")
	require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "KonnectReferencesResolved").Message, "root schemaValidation")
	assertNoChainWrites(t, remote, before)
	parent.Spec.APISpec = *original
	require.NoError(t, cl.Update(ctx, parent))
	programPolicy(parent, "parent-id")
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, want)

	// With resync and periodic reconciliation disabled, unlisted policies must
	// still invalidate and then restore the complete scope through watches.
	remote.setPolicies("parent-id", "second-id", "first-id", "root-id", "extra-id")
	extra := createPolicy("extra", "")
	programPolicy(extra, "extra-id")
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(chain.Status.Conditions, "Programmed").Message, "complete remote produce policy chain")
	before = len(remote.writes())
	assertNoChainWrites(t, remote, before)
	remote.setPolicies(want...)
	require.NoError(t, cl.Delete(ctx, extra))
	waitForProduceChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, want)

	require.NoError(t, cl.Delete(ctx, auth))
	require.NoError(t, cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.NotContains(t, vc.Annotations, policychain.ProduceOwnerAnnotation)
	require.Equal(t, "independent-cluster/uid", vc.Annotations[policychain.OwnerAnnotation])
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(first), first))
	require.Equal(t, "parent", first.Spec.APISpec.ParentPolicyRef.Name)
	remote.mu.Lock()
	defer remote.mu.Unlock()
	require.Equal(t, want, remote.policies)
}

func waitForProduceChainCondition(t *testing.T, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain, conditionType string, status metav1.ConditionStatus, reason string) {
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
