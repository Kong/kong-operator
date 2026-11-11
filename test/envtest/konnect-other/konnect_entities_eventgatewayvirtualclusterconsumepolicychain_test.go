package konnectother

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
	"sigs.k8s.io/yaml"

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

func TestEventGatewayVirtualClusterConsumePolicyChainNestedRecovery(t *testing.T) {
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
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))
	remote := &policyChainRemote{policies: []string{"root-id", "parent-id", "mask-id", "transcode-id", "skip-id"}}
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
			remote.mu.Lock()
			defer remote.mu.Unlock()
			return &sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote.policies)},
			}, nil
		})
	sdk.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
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
				owner: parent.Annotations[policychain.ConsumeOwnerAnnotation], policies: slices.Clone(requested),
			})
			remote.policies = slices.Clone(requested)
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(requested)},
			}, nil
		})
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
		Return(&sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil)
	factory.SDK.EventGatewayVirtualClusterPoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil)
	factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
		Return(&sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil)
	factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
			EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{}},
		}, nil)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](&metricsmocks.MockRecorder{})),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](&metricsmocks.MockRecorder{})),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterPolicyChain](&metricsmocks.MockRecorder{})),
	)
	t.Cleanup(cancel)
	produce, cluster := newProduceChain(vc, "produce"), newPolicyChain(vc, "cluster")
	cluster.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
	require.NoError(t, cl.Create(ctx, produce))
	require.NoError(t, cl.Create(ctx, cluster))
	waitForProduceChainCondition(t, cl, produce, "Programmed", metav1.ConditionTrue, "Programmed")
	waitForChainCondition(t, cl, cluster, "Programmed", metav1.ConditionTrue, "Programmed")

	createPolicy := func(name, spec string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
		t.Helper()
		p := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: name, Namespace: ns.Name,
			Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name}},
			},
		}
		require.NoError(t, yaml.UnmarshalStrict([]byte(spec), &p.Spec.APISpec))
		require.NoError(t, cl.Create(ctx, p))
		return p
	}
	programPolicy := func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) {
		t.Helper()
		p.SetKonnectID(p.Name + "-id")
		p.SetGatewayID("gateway-id")
		p.SetVirtualClusterID("vc-id")
		p.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(p.Generation)}
		require.NoError(t, cl.Status().Update(ctx, p))
	}
	parent := createPolicy("parent", `type: schemaValidation
schemaValidation: {name: parent, config: {type: json, json: {valueValidationAction: skip}}}`)
	mask := createPolicy("mask", `type: maskFields
parentPolicyRef: {name: parent}
maskFields: {name: mask, config: {failureMode: skip, maskFields: [{paths: {type: array, array: [{match: email}]}, strategy: {type: replace, replace: {phrase: redacted}}}]}}`)
	transcode := createPolicy("transcode", `type: transcode
parentPolicyRef: {name: parent}
transcode:
  name: transcode
  config:
    outputFormat: json
    json:
      failureMode: skip
      schemaSource:
        type: reference
        reference:
          subject: orders-value
          version: latest
          schemaRefDestination: {type: none, none: {}}
          schemaRegistry: {type: namespacedRef, namespacedRef: {name: orders}}`)
	root := createPolicy("root", `type: skipRecord
skipRecord: {name: root}`)
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{parent, mask, transcode, root} {
		programPolicy(p)
	}
	chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{Name: "chain", Namespace: ns.Name,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainSpec{
			EventGatewayVirtualClusterRef: parent.GetParentRef(),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "parent"}, {Name: "transcode"}, {Name: "skip"}, {Name: "mask"}, {Name: "root"}},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, chain))
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	assertNoChainWrites(t, remote, 0)
	skip := createPolicy("skip", `type: skipRecord
parentPolicyRef: {name: parent}
skipRecord: {name: skip}`)
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	programPolicy(skip)
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	want := []string{"parent-id", "transcode-id", "skip-id", "mask-id", "root-id"}
	assertChainWrites(t, remote, 0, want)
	require.Empty(t, chain.GetKonnectID())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.Equal(t, policychain.ClaimValue(chain), vc.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(produce), vc.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), vc.Annotations[policychain.OwnerAnnotation])

	before := len(remote.writes())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	parent.Spec.APISpec.ConsumeSchemaValidationPolicy.Name = "renamed-parent"
	require.NoError(t, cl.Update(ctx, parent))
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotProgrammed")
	assertNoChainWrites(t, remote, before)
	programPolicy(parent)
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertNoChainWrites(t, remote, before)
	for _, invalid := range [][]string{
		{"mask", "parent", "transcode", "skip", "root"},
		{"parent", "mask", "root", "transcode", "skip"},
		{"parent", "transcode", "skip", "root", "mask"},
	} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		chain.Spec.APISpec.Policies = nil
		for _, name := range invalid {
			chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: name})
		}
		require.NoError(t, cl.Update(ctx, chain))
		waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceInvalid")
		assertNoChainWrites(t, remote, before)
	}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{
		{Name: "parent"}, {Name: "transcode"}, {Name: "skip"}, {Name: "mask"}, {Name: "root"},
	}
	require.NoError(t, cl.Update(ctx, chain))
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertNoChainWrites(t, remote, before)
	contender := chain.DeepCopy()
	contender.ObjectMeta = metav1.ObjectMeta{Name: "contender", Namespace: ns.Name}
	contender.Status = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainStatus{}
	require.NoError(t, cl.Create(ctx, contender))
	waitForConsumeChainCondition(t, cl, contender, "PolicyChainOwnerValid", metav1.ConditionFalse, "ConflictingChain")
	assertNoChainWrites(t, remote, before)
	require.NoError(t, cl.Delete(ctx, contender))
	// Scope-wide watches must detect unlisted additions and deletions even
	// with periodic reconciliation and cache resync effectively disabled.
	remote.setPolicies(append(slices.Clone(want), "extra-id")...)
	extra := createPolicy("extra", `type: skipRecord
skipRecord: {name: extra}`)
	programPolicy(extra)
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionFalse, "")
	require.Contains(t, meta.FindStatusCondition(chain.GetConditions(), "Programmed").Message, "complete remote consume policy chain")
	assertNoChainWrites(t, remote, before)
	remote.setPolicies("root-id", "parent-id", "mask-id", "transcode-id", "skip-id")
	require.NoError(t, cl.Delete(ctx, extra))
	waitForConsumeChainCondition(t, cl, chain, "Programmed", metav1.ConditionTrue, "Programmed")
	assertChainWrites(t, remote, before, want)

	before = len(remote.writes())
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "missing"}}
	require.NoError(t, cl.Update(ctx, chain))
	waitForConsumeChainCondition(t, cl, chain, "KonnectReferencesResolved", metav1.ConditionFalse, "ReferenceNotFound")
	require.NoError(t, cl.Delete(ctx, auth))
	require.NoError(t, cl.Delete(ctx, chain))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	}, consts.WaitTime, consts.TickTime)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(vc), vc))
	require.NotContains(t, vc.Annotations, policychain.ConsumeOwnerAnnotation)
	require.Equal(t, policychain.ClaimValue(produce), vc.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), vc.Annotations[policychain.OwnerAnnotation])
	assertNoChainWrites(t, remote, before)
	for _, p := range []*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{mask, transcode, skip} {
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(p), p))
		require.Equal(t, "parent", p.Spec.APISpec.ParentPolicyRef.Name)
	}
}

func waitForConsumeChainCondition(t *testing.T, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain, conditionType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain)) {
			return
		}
		c := meta.FindStatusCondition(chain.GetConditions(), conditionType)
		if assert.NotNil(ct, c) {
			assert.Equal(ct, status, c.Status, "%s", c.Message)
			assert.Equal(ct, chain.Generation, c.ObservedGeneration, "%s", c.Message)
			if reason != "" {
				assert.Equal(ct, reason, c.Reason, "%s", c.Message)
			}
		}
	}, consts.WaitTime, consts.TickTime)
}
