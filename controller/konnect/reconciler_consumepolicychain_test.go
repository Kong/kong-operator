package konnect

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func testConsumePolicyChain(name string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain {
	return &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{
		Name: name, Namespace: "default", UID: types.UID(name + "-uid"), Generation: 1,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainSpec{
			EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{},
			},
		},
	}
}

func TestConsumePolicyChainOnPremReconciliationIsSkipped(t *testing.T) {
	t.Parallel()
	chain := testConsumePolicyChain("chain")
	backend := newTestOnPremEventGatewayBackendCluster(chain.Namespace, "backend", "onprem-gw")
	parent := newTestEventGatewayVirtualCluster(chain.Namespace, "vc", backend.Name)
	cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(backend, parent, chain).Build()
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](nil, logging.DevelopmentMode, cl)
	for range 3 {
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain))
		result, err := r.Reconcile(t.Context(), chain)
		require.NoError(t, err, "on-prem chains must not use a Konnect SDK factory or authentication")
		require.True(t, result.IsZero())
	}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(chain), chain))
	require.Empty(t, chain.Finalizers)
	require.Empty(t, chain.GetKonnectID())
	require.Empty(t, chain.GetConditions())
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policychain.ConsumeOwnerAnnotation)
}

func TestConsumePolicyChainReconcileSnapshotsAndContender(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	chain := testConsumePolicyChain("chain")
	var policies []client.Object
	for _, name := range []string{"parent", "second", "first", "root"} {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: name})
		p := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: name, Namespace: chain.Namespace, Generation: 1,
			Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
				EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc"),
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
					EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
						Type:                   configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeSkipRecordPolicyCreate,
						SkipRecordPolicyCreate: &configurationv1alpha1.EventGatewaySkipRecordPolicyCreate{Name: name},
					},
				},
			},
		}
		if name == "parent" {
			p.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig = &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
				Type:                          configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy,
				ConsumeSchemaValidationPolicy: &configurationv1alpha1.EventGatewayConsumeSchemaValidationPolicy{},
			}
		} else if name != "root" {
			p.Spec.APISpec.ParentPolicyRef.Name = "parent"
		}
		p.SetKonnectID(name + "-id")
		p.SetGatewayID("gateway-id")
		p.SetVirtualClusterID("vc-id")
		ops.SetKonnectEntityProgrammedConditionTrue(p)
		policies = append(policies, p)
	}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{configurationv1alpha1.GroupVersion, konnectv1alpha1.GroupVersion})
	for _, kind := range []string{"EventGatewayVirtualClusterConsumePolicyChain", "EventGatewayVirtualClusterConsumePolicy", "EventGatewayVirtualCluster", "EventGatewayBackendCluster", "KongReferenceGrant"} {
		mapper.Add(configurationv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	for _, kind := range []string{"KonnectAPIAuthConfiguration", "KonnectEventGateway"} {
		mapper.Add(konnectv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	reads := map[string]int{}
	cl := newFakeClientBuilder(scheme.Get()).WithRESTMapper(mapper).
		WithStatusSubresource(chain, &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		WithObjects(append(policies, chain)...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy); ok {
				reads[key.Name]++
			}
			return cl.Get(ctx, key, obj, opts...)
		},
	}).Build()
	nsClient := client.NewNamespacedClient(cl, chain.Namespace)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, nsClient)
	gateway := deploy.KonnectEventGateway(t, ctx, nsClient, auth)
	backend := deploy.EventGatewayBackendCluster(t, ctx, nsClient, gateway)
	parent := deploy.EventGatewayVirtualCluster(t, ctx, nsClient, backend, deploy.WithName("vc"))
	parent.SetKonnectID("vc-id")
	parent.SetGatewayID("gateway-id")
	ops.SetKonnectEntityProgrammedConditionTrue(parent)
	require.NoError(t, cl.Status().Update(ctx, parent))
	factory := sdkmocks.NewMockSDKFactory(t)
	remote := []string{"root-id", "parent-id", "first-id", "second-id"}
	var applied [][]string
	sdk := factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
			return &sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	sdk.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
			remote = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			applied = append(applied, slices.Clone(remote))
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	r := NewKonnectEntityReconciler(factory, logging.DevelopmentMode, cl,
		WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](time.Hour),
		WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](&metricsmocks.MockRecorder{}),
	)
	run := func(c *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain) error {
		t.Helper()
		reads = map[string]int{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(c), c))
		c.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(c.GetTypeName()))
		_, err := r.Reconcile(ctx, c)
		for name, count := range reads {
			require.Equal(t, 1, count, "references and group validation share the snapshot of %s", name)
		}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(c), c))
		return err
	}
	untilApplied := func(want []string) {
		t.Helper()
		before := len(applied)
		for range 20 {
			require.NoError(t, run(chain))
			if len(applied) > before {
				require.Equal(t, want, applied[len(applied)-1])
				require.Len(t, reads, 4)
				require.True(t, entityHasProgrammedStatusTrue(chain))
				require.Empty(t, chain.GetKonnectID())
				return
			}
		}
		t.Fatalf("chain not applied: %+v", chain.Status.Conditions)
	}
	want := []string{"parent-id", "second-id", "first-id", "root-id"}
	untilApplied(want)
	before := len(applied)
	condition := *apimeta.FindStatusCondition(chain.GetConditions(), "Programmed")
	require.NoError(t, run(chain))
	require.Len(t, applied, before)
	require.Equal(t, condition, *apimeta.FindStatusCondition(chain.GetConditions(), "Programmed"))
	contender := testConsumePolicyChain("contender")
	contender.Spec = *chain.Spec.DeepCopy()
	require.NoError(t, cl.Create(ctx, contender))
	for range 12 {
		require.NoError(t, run(contender))
	}
	require.Len(t, applied, before, "the losing claimant must never PUT")
	require.True(t, apimeta.IsStatusConditionFalse(contender.GetConditions(), "PolicyChainOwnerValid"))
	remote = []string{"root-id", "parent-id", "first-id", "second-id"}
	untilApplied(want)
	child := policies[2].(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(child), child))
	child.Generation++
	require.NoError(t, cl.Update(ctx, child))
	before = len(applied)
	require.NoError(t, run(chain))
	require.Len(t, applied, before)
	require.True(t, apimeta.IsStatusConditionFalse(chain.GetConditions(), commonv1alpha1.KonnectReferencesResolvedConditionType))
	child.SetKonnectID("recreated-first-id")
	ops.SetKonnectEntityProgrammedConditionTrue(child)
	require.NoError(t, cl.Status().Update(ctx, child))
	for range 3 {
		if err := run(chain); err != nil {
			require.ErrorContains(t, err, "complete remote consume policy chain")
		}
	}
	require.Len(t, applied, before, "an old remote ID must block PUT")
	remote = []string{"root-id", "parent-id", "second-id", "recreated-first-id"}
	untilApplied([]string{"parent-id", "second-id", "recreated-first-id", "root-id"})
}

func TestConsumePolicyChainIndependentClaimsAndRetention(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	consume, produce, cluster := testConsumePolicyChain("consume"), testProducePolicyChain("produce"), testPolicyChain("cluster")
	parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default"}
	cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(consume, produce, cluster).WithObjects(parent, consume, produce, cluster).Build()
	for _, chain := range []policyChain{consume, produce, cluster} {
		owned, _, err := claimPolicyChain(ctx, cl, chain, time.Hour)
		require.NoError(t, err)
		require.True(t, owned)
	}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policychain.ClaimValue(consume), parent.Annotations[policychain.ConsumeOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(produce), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), parent.Annotations[policychain.OwnerAnnotation])
	controllerutil.AddFinalizer(consume, KonnectCleanupFinalizer)
	consume.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "missing"}}
	require.NoError(t, cl.Update(ctx, consume))
	require.NoError(t, cl.Delete(ctx, consume))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(consume), consume))
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](nil, logging.DevelopmentMode, cl)
	_, err := r.Reconcile(ctx, consume)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(consume), consume)))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policychain.ConsumeOwnerAnnotation)
	require.Equal(t, policychain.ClaimValue(produce), parent.Annotations[policychain.ProduceOwnerAnnotation])
	require.Equal(t, policychain.ClaimValue(cluster), parent.Annotations[policychain.OwnerAnnotation])
}

func TestConsumePolicyChainLiveClaims(t *testing.T) {
	t.Parallel()
	for _, claim := range []string{"owner/owner-uid", "owner/previous-uid", "missing/uid", "malformed", "bad/name/uid"} {
		t.Run(claim, func(t *testing.T) {
			chain, owner := testConsumePolicyChain("chain"), testConsumePolicyChain("owner")
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default",
				Annotations: map[string]string{policychain.ConsumeOwnerAnnotation: claim},
			}
			cached := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(chain, parent).Build()
			live := newFakeClientBuilder(scheme.Get()).WithObjects(chain, owner, parent).Build()
			owned, _, err := claimPolicyChain(t.Context(), policyChainLiveGetClient{Client: cached, apiReader: live}, chain, consts.DefaultKonnectSyncPeriod)
			require.NoError(t, err)
			require.Equal(t, claim != policychain.ClaimValue(owner), owned)
		})
	}
	for _, state := range []string{"recreated", "new generation", "terminating", "CAS conflict"} {
		t.Run(state, func(t *testing.T) {
			chain := testConsumePolicyChain("chain")
			current := chain.DeepCopy()
			switch state {
			case "recreated":
				current.UID = "new-uid"
			case "new generation":
				current.Generation++
			case "terminating":
				current.DeletionTimestamp = new(metav1.Now())
				current.Finalizers = []string{KonnectCleanupFinalizer}
			}
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: "default"}
			cached := newFakeClientBuilder(scheme.Get()).WithObjects(chain, parent).WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, _ ...client.PatchOption) error {
					require.Equal(t, "CAS conflict", state)
					data, err := patch.Data(obj)
					require.NoError(t, err)
					require.Contains(t, string(data), `"resourceVersion"`, "claims must use a compare-and-swap patch")
					return apierrors.NewConflict(configurationv1alpha1.GroupVersion.WithResource("eventgatewayvirtualclusters").GroupResource(), "vc", nil)
				},
			}).Build()
			live := newFakeClientBuilder(scheme.Get()).WithObjects(current, parent).Build()
			owned, res, err := claimPolicyChain(t.Context(), policyChainLiveGetClient{Client: cached, apiReader: live}, chain, time.Hour)
			require.NoError(t, err)
			require.False(t, owned)
			require.True(t, res.Requeue)
			require.NoError(t, cached.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			require.NotContains(t, parent.Annotations, policychain.ConsumeOwnerAnnotation)
		})
	}
}

func TestConsumePolicyChainScopeWatch(t *testing.T) {
	t.Parallel()
	listed, empty, other := testConsumePolicyChain("listed"), testConsumePolicyChain("empty"), testConsumePolicyChain("other")
	listed.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "policy"}}
	other.Spec.EventGatewayVirtualClusterRef = testNamespacedObjectRef("other-vc")
	builder := newFakeClientBuilder(scheme.Get()).WithObjects(listed, empty, other)
	for _, opt := range index.OptionsForEventGatewayVirtualClusterConsumePolicyChain() {
		builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()
	policy := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: "policy", Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc")},
	}
	watch := enqueueEventGatewayVirtualClusterConsumePolicyChainForEventGatewayVirtualClusterConsumePolicy(cl)
	for _, name := range []string{"policy", "unlisted"} {
		policy.Name = name
		requests := watch(t.Context(), policy)
		require.Contains(t, requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(listed)})
		require.Contains(t, requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(empty)})
		require.NotContains(t, requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(other)})
	}
}
