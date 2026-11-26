package konnect

import (
	"context"
	"slices"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/internal/utils/policychain"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// sideChainCrossNamespaceCase is a produce or consume policy chain of the
// namespace "default" ordering, on the virtual cluster "vc" of the namespace
// "parent", the single policy "policy" of the namespace "policies".
type sideChainCrossNamespaceCase[TEnt any] struct {
	chainKind, policyKind, ownerAnnotation string
	// chain returns a chain of name of the namespace "default", referencing
	// the virtual cluster "vc" of the namespace "parent" and ordering the
	// policy "policy" of the namespace "policies".
	chain func(name string) TEnt
	// policy is the programmed policy "policy" of the namespace "policies".
	policy client.Object
	// expectSDK sets the expectations of the SDK of the chain, recording
	// the policy IDs applied.
	expectSDK func(factory *sdkmocks.MockSDKFactory, applied *[][]string)
}

// testSideChainCrossNamespaceGrantedLifecycle checks a produce or consume
// policy chain referencing a virtual cluster and a policy of other
// namespaces: each needs a KongReferenceGrant in its namespace, and once the
// one of the virtual cluster is revoked, a chain of its namespace can claim
// it.
func testSideChainCrossNamespaceGrantedLifecycle[
	T constraints.SupportedKonnectEntityType,
	TEnt interface {
		constraints.EntityType[T]
		policychain.Chain
		GetConditions() []metav1.Condition
	},
](t *testing.T, c sideChainCrossNamespaceCase[TEnt]) {
	ctx := t.Context()
	chain := c.chain("chain")
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{configurationv1alpha1.GroupVersion, konnectv1alpha1.GroupVersion})
	for _, kind := range []string{c.chainKind, c.policyKind, "EventGatewayVirtualCluster", "EventGatewayBackendCluster", "KongReferenceGrant"} {
		mapper.Add(configurationv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	for _, kind := range []string{"KonnectAPIAuthConfiguration", "KonnectEventGateway"} {
		mapper.Add(konnectv1alpha1.GroupVersion.WithKind(kind), apimeta.RESTScopeNamespace)
	}
	cl := newFakeClientBuilder(scheme.Get()).WithRESTMapper(mapper).
		WithStatusSubresource(chain, c.policy, &configurationv1alpha1.EventGatewayVirtualCluster{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		WithObjects(chain, c.policy).Build()
	parentClient := client.NewNamespacedClient(cl, "parent")
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, parentClient)
	gateway := deploy.KonnectEventGateway(t, ctx, parentClient, auth)
	backend := deploy.EventGatewayBackendCluster(t, ctx, parentClient, gateway)
	parent := deploy.EventGatewayVirtualCluster(t, ctx, parentClient, backend, deploy.WithName("vc"))
	parent.SetKonnectID("vc-id")
	parent.SetGatewayID("gateway-id")
	ops.SetKonnectEntityProgrammedConditionTrue(parent)
	require.NoError(t, cl.Status().Update(ctx, parent))
	grant := func(namespace, toKind string) *configurationv1alpha1.KongReferenceGrant {
		return deploy.KongReferenceGrant(t, ctx, client.NewNamespacedClient(cl, namespace),
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:  configurationv1alpha1.Kind(c.chainKind), Namespace: "default",
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: configurationv1alpha1.Kind(toKind),
			}),
		)
	}

	factory := sdkmocks.NewMockSDKFactory(t)
	var applied [][]string
	c.expectSDK(factory, &applied)
	r := NewKonnectEntityReconciler(factory, logging.DevelopmentMode, cl,
		WithMetricRecorder[T, TEnt](&metricsmocks.MockRecorder{}),
	)
	reconcileUntil := func(done func() bool) {
		t.Helper()
		for range 20 {
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
			chain.GetObjectKind().SetGroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(c.chainKind))
			_, err := r.Reconcile(ctx, chain)
			require.NoError(t, err)
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
			if done() {
				return
			}
		}
		require.FailNow(t, "the chain didn't reach the expected state", "conditions: %v", chain.GetConditions())
	}

	// Without a KongReferenceGrant in the namespace of the virtual cluster,
	// the chain neither claims it nor is applied.
	reconcileUntil(func() bool {
		return apimeta.IsStatusConditionFalse(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)
	})
	notGranted := apimeta.FindStatusCondition(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)
	require.Equal(t, string(configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted), notGranted.Reason)
	require.False(t, apimeta.IsStatusConditionTrue(chain.GetConditions(), "Programmed"))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.NotContains(t, parent.Annotations, c.ownerAnnotation)
	require.Empty(t, applied)

	// Without one in the namespace of the policy, the chain isn't applied.
	vcGrant := grant("parent", "EventGatewayVirtualCluster")
	reconcileUntil(func() bool {
		return apimeta.IsStatusConditionFalse(chain.GetConditions(), commonv1alpha1.KonnectReferencesResolvedConditionType)
	})
	resolved := apimeta.FindStatusCondition(chain.GetConditions(), commonv1alpha1.KonnectReferencesResolvedConditionType)
	require.Equal(t, commonv1alpha1.KonnectReferencesResolvedReasonNotPermitted, resolved.Reason)
	require.Contains(t, resolved.Message, c.policyKind+" policy in namespace policies")
	require.Contains(t, resolved.Message, "not granted by any KongReferenceGrant")
	require.False(t, apimeta.IsStatusConditionTrue(chain.GetConditions(), "Programmed"))
	require.Empty(t, applied)

	grant("policies", c.policyKind)
	reconcileUntil(func() bool { return apimeta.IsStatusConditionTrue(chain.GetConditions(), "Programmed") })
	require.True(t, apimeta.IsStatusConditionTrue(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
	require.True(t, apimeta.IsStatusConditionTrue(chain.GetConditions(), commonv1alpha1.KonnectReferencesResolvedConditionType))
	// The remote chain already lists the policy: an order that matches needs
	// no PUT.
	for _, ids := range applied {
		require.Equal(t, []string{"policy-id"}, ids)
	}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, "default/chain/chain-uid", parent.Annotations[c.ownerAnnotation], "the claim names the namespace of the chain")

	// Once the virtual cluster grant is revoked, the chain no longer resolves
	// its virtual cluster, and its claim is stale: a chain of the namespace
	// of the virtual cluster can take it.
	require.NoError(t, cl.Delete(ctx, vcGrant))
	reconcileUntil(func() bool {
		return apimeta.IsStatusConditionFalse(chain.GetConditions(), configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)
	})
	local := c.chain("local")
	local.SetNamespace("parent")
	require.NoError(t, cl.Create(ctx, local))
	owner, err := policychain.Claim(ctx, cl, local)
	require.NoError(t, err)
	require.Empty(t, owner, "a chain no KongReferenceGrant allows anymore doesn't hold its claim")
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policychain.ClaimValue(local), parent.Annotations[c.ownerAnnotation])

	require.NoError(t, cl.Delete(ctx, chain))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(chain), chain))
	_, err = r.Reconcile(ctx, chain)
	require.NoError(t, err)
	require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(chain), chain)))
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	require.Equal(t, policychain.ClaimValue(local), parent.Annotations[c.ownerAnnotation], "deleting the chain leaves the claim of another")
}

// crossNamespaceVirtualClusterRef references the virtual cluster "vc" of the
// namespace "parent".
func crossNamespaceVirtualClusterRef() commonv1alpha1.ObjectRef {
	ref := testNamespacedObjectRef("vc")
	ref.NamespacedRef.Namespace = new("parent")
	return ref
}

func TestProducePolicyChainCrossNamespaceGrantedLifecycle(t *testing.T) {
	t.Parallel()
	policy := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
		Name: "policy", Namespace: "policies", Generation: 1,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
			EventGatewayVirtualClusterRef: crossNamespaceVirtualClusterRef(),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
				EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
					Type:                      configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{},
				},
			},
		},
	}
	policy.SetKonnectID("policy-id")
	policy.SetGatewayID("gateway-id")
	policy.SetVirtualClusterID("vc-id")
	ops.SetKonnectEntityProgrammedConditionTrue(policy)
	testSideChainCrossNamespaceGrantedLifecycle[configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain](t,
		sideChainCrossNamespaceCase[*configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain]{
			chainKind: "EventGatewayVirtualClusterProducePolicyChain", policyKind: "EventGatewayVirtualClusterProducePolicy",
			ownerAnnotation: policychain.ProduceOwnerAnnotation,
			chain: func(name string) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain {
				chain := testProducePolicyChain(name)
				chain.Spec.EventGatewayVirtualClusterRef = crossNamespaceVirtualClusterRef()
				chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{{Name: "policy", Namespace: "policies"}}
				return chain
			},
			policy: policy,
			expectSDK: func(factory *sdkmocks.MockSDKFactory, applied *[][]string) {
				sdk := factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK
				sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
					Return(&sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
						EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{"policy-id"}},
					}, nil).Maybe()
				sdk.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse, error) {
						*applied = append(*applied, slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies))
						return &sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
							EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: req.UpdateEventGatewayPolicyChainRequest.Policies},
						}, nil
					}).Maybe()
			},
		})
}

func TestConsumePolicyChainCrossNamespaceGrantedLifecycle(t *testing.T) {
	t.Parallel()
	policy := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
		Name: "policy", Namespace: "policies", Generation: 1,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
			EventGatewayVirtualClusterRef: crossNamespaceVirtualClusterRef(),
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
					Type:                      configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{},
				},
			},
		},
	}
	policy.SetKonnectID("policy-id")
	policy.SetGatewayID("gateway-id")
	policy.SetVirtualClusterID("vc-id")
	ops.SetKonnectEntityProgrammedConditionTrue(policy)
	testSideChainCrossNamespaceGrantedLifecycle[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain](t,
		sideChainCrossNamespaceCase[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain]{
			chainKind: "EventGatewayVirtualClusterConsumePolicyChain", policyKind: "EventGatewayVirtualClusterConsumePolicy",
			ownerAnnotation: policychain.ConsumeOwnerAnnotation,
			chain: func(name string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain {
				chain := testConsumePolicyChain(name)
				chain.Spec.EventGatewayVirtualClusterRef = crossNamespaceVirtualClusterRef()
				chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "policy", Namespace: "policies"}}
				return chain
			},
			policy: policy,
			expectSDK: func(factory *sdkmocks.MockSDKFactory, applied *[][]string) {
				sdk := factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK
				sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", "vc-id").
					Return(&sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{
						EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: []string{"policy-id"}},
					}, nil).Maybe()
				sdk.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, mock.Anything).
					RunAndReturn(func(_ context.Context, req sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse, error) {
						*applied = append(*applied, slices.Clone(req.UpdateEventGatewayPolicyChainRequest.Policies))
						return &sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{
							EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: req.UpdateEventGatewayPolicyChainRequest.Policies},
						}, nil
					}).Maybe()
			},
		})
}
