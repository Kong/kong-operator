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

func testProducePolicyChain(name string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain {
	return &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
		Name: name, Namespace: "default", UID: types.UID(name + "-uid"), Generation: 1,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainSpec{
			EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc"),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChainAPISpec{
				Policies: []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{},
			},
		},
	}
}

func TestProducePolicyChainReconcileSnapshots(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	chain := testProducePolicyChain("chain")
	var policies []client.Object
	for _, name := range []string{"parent", "second", "first", "root"} {
		chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: name})
		p := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: name, Namespace: chain.Namespace, Generation: 1,
			Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
				EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc"),
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
					EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
						Type:                      configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
						ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{},
					},
				},
			},
		}
		if name == "parent" {
			p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
				Type:                          configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy,
				ProduceSchemaValidationPolicy: &configurationv1alpha1.EventGatewayProduceSchemaValidationPolicy{},
			}
		} else if name != "root" {
			p.Spec.APISpec.ParentPolicyRef.Name = "parent"
			if name == "first" {
				p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
					Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordMaskFieldsProducePolicyCreate,
					ParsedRecordMaskFieldsProducePolicyCreate: &configurationv1alpha1.EventGatewayParsedRecordMaskFieldsProducePolicyCreate{},
				}
			} else {
				p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig = &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
					Type:                                     configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeParsedRecordTranscodeProducePolicyCreate,
					ParsedRecordTranscodeProducePolicyCreate: &configurationv1alpha1.EventGatewayParsedRecordTranscodeProducePolicyCreate{},
				}
			}
		}
		p.SetKonnectID(name + "-id")
		p.SetGatewayID("gateway-id")
		p.SetVirtualClusterID("vc-id")
		ops.SetKonnectEntityProgrammedConditionTrue(p)
		policies = append(policies, p)
	}
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{configurationv1alpha1.GroupVersion, konnectv1alpha1.GroupVersion})
	for _, kind := range []string{"EventGatewayVirtualClusterProducePolicyChain", "EventGatewayVirtualClusterProducePolicy", "EventGatewayVirtualCluster", "EventGatewayBackendCluster", "KongReferenceGrant"} {
		mapper.Add(configurationv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	for _, kind := range []string{"KonnectAPIAuthConfiguration", "KonnectEventGateway"} {
		mapper.Add(konnectv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	reads := make(map[string]int)
	cl := newFakeClientBuilder(scheme.Get()).WithRESTMapper(mapper).
		WithStatusSubresource(chain, &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		WithObjects(append(policies, chain)...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy); ok {
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
	sdk := factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK
	sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
		RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			return &sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	sdk.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse, error) {
			remote = slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies)
			applied = append(applied, slices.Clone(remote))
			return &sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
				EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: slices.Clone(remote)},
			}, nil
		})
	r := NewKonnectEntityReconciler(factory, logging.DevelopmentMode, cl,
		WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](0),
		WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](&metricsmocks.MockRecorder{}),
	)
	run := func() error {
		t.Helper()
		reads = make(map[string]int)
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		chain.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(chain.GetTypeName()))
		_, err := r.Reconcile(ctx, chain)
		for name, count := range reads {
			require.Equal(t, 1, count, "snapshot for %s must be shared between reference, nested and SDK validation", name)
		}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
		return err
	}
	untilApplied := func(want []string) {
		t.Helper()
		before := len(applied)
		for range 20 {
			require.NoError(t, run())
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
	remote = []string{"root-id", "parent-id", "first-id", "second-id"}
	untilApplied(want)
	child := policies[2].(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy)
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(child), child))
	child.Generation++
	require.NoError(t, cl.Update(ctx, child))
	before := len(applied)
	require.NoError(t, run())
	require.Len(t, applied, before)
	require.True(t, apimeta.IsStatusConditionFalse(chain.Status.Conditions, commonv1alpha1.KonnectReferencesResolvedConditionType))
	require.False(t, entityHasProgrammedStatusTrue(chain))
	child.SetKonnectID("recreated-first-id")
	ops.SetKonnectEntityProgrammedConditionTrue(child)
	require.NoError(t, cl.Status().Update(ctx, child))
	remote = []string{"parent-id", "first-id", "second-id", "root-id"}
	for range 3 {
		if err := run(); err != nil {
			require.ErrorContains(t, err, "complete remote produce policy chain")
		}
	}
	require.Len(t, applied, before, "old remote ID must block PUT")
	remote = []string{"parent-id", "second-id", "recreated-first-id", "root-id"}
	untilApplied(remote)
}

func TestProducePolicyChainOwnershipAndRetention(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	first, second := testProducePolicyChain("first"), testProducePolicyChain("second")
	parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: first.Namespace,
		Annotations: map[string]string{policychain.OwnerAnnotation: "cluster/uid", policychain.ListenerOwnerAnnotation: "listener/uid"},
	}
	cl := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(first, second).WithObjects(parent, first, second).Build()
	owned, _, err := claimPolicyChain(ctx, cl, first, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.True(t, owned)
	owned, res, err := claimPolicyChain(ctx, cl, second, 7*time.Second)
	require.NoError(t, err)
	require.False(t, owned)
	require.Equal(t, 7*time.Second, res.RequeueAfter)
	require.Equal(t, "ConflictingChain", apimeta.FindStatusCondition(second.Status.Conditions, "PolicyChainOwnerValid").Reason)
	require.False(t, entityHasProgrammedStatusTrue(second))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policychain.ClaimValue(first), parent.Annotations[policychain.ProduceOwnerAnnotation])
	controllerutil.AddFinalizer(first, KonnectCleanupFinalizer)
	first.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{{Name: "missing"}}
	require.NoError(t, cl.Update(ctx, first))
	require.NoError(t, cl.Delete(ctx, first))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(first), first))
	r := NewKonnectEntityReconciler[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](nil, logging.DevelopmentMode, cl)
	_, err = r.Reconcile(ctx, first)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(first), first)))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, policychain.ProduceOwnerAnnotation)
	require.Equal(t, "cluster/uid", parent.Annotations[policychain.OwnerAnnotation])
	require.Equal(t, "listener/uid", parent.Annotations[policychain.ListenerOwnerAnnotation])
	owned, _, err = claimPolicyChain(ctx, cl, second, consts.DefaultKonnectSyncPeriod)
	require.NoError(t, err)
	require.True(t, owned)
}

func TestProducePolicyChainLiveOwnership(t *testing.T) {
	t.Parallel()
	chain, owner := testProducePolicyChain("chain"), testProducePolicyChain("owner")
	for _, claim := range []string{policychain.ClaimValue(owner), "owner/previous-uid", "missing/uid", "malformed"} {
		t.Run(claim, func(t *testing.T) {
			parent := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: chain.Namespace,
				Annotations: map[string]string{policychain.ProduceOwnerAnnotation: claim},
			}
			cached := newFakeClientBuilder(scheme.Get()).WithStatusSubresource(chain).WithObjects(chain, parent).Build()
			live := newFakeClientBuilder(scheme.Get()).WithObjects(chain, owner, parent).Build()
			owned, _, err := claimPolicyChain(t.Context(), policyChainLiveGetClient{Client: cached, apiReader: live}, chain, consts.DefaultKonnectSyncPeriod)
			require.NoError(t, err)
			require.Equal(t, claim != policychain.ClaimValue(owner), owned, "a live owner missing only from cache is not stale")
		})
	}
}

func TestProducePolicyChainScopeWatch(t *testing.T) {
	t.Parallel()
	listed, empty, other := testProducePolicyChain("listed"), testProducePolicyChain("empty"), testProducePolicyChain("other")
	listed.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{{Name: "policy"}}
	other.Spec.EventGatewayVirtualClusterRef = testNamespacedObjectRef("other-vc")
	builder := newFakeClientBuilder(scheme.Get()).WithObjects(listed, empty, other)
	for _, opt := range index.OptionsForEventGatewayVirtualClusterProducePolicyChain() {
		builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()
	policy := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: "policy", Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{EventGatewayVirtualClusterRef: testNamespacedObjectRef("vc")},
	}
	watch := enqueueEventGatewayVirtualClusterProducePolicyChainForEventGatewayVirtualClusterProducePolicy(cl)
	for _, name := range []string{"policy", "unlisted"} {
		policy.Name = name
		reqs := watch(t.Context(), policy)
		require.Contains(t, reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(listed)})
		require.Contains(t, reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(empty)})
		require.NotContains(t, reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(other)})
	}
}
