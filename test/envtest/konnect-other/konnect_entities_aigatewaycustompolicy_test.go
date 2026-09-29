package konnectother

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestAIGatewayCustomPolicyLuaFromConfigMap(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[aiconfigurationv1alpha1.AIGatewayCustomPolicy](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[aiconfigurationv1alpha1.AIGatewayCustomPolicy](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and parent KonnectAIGateway")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	gateway := deploy.KonnectAIGateway(t, ctx, clientNamespaced, apiAuth)

	const konnectAIGatewayID = "ai-gw-cp-custom-policy"
	updateKonnectAIGatewayStatusWithProgrammed(t, ctx, clientNamespaced, gateway, konnectAIGatewayID)

	const (
		customPolicyID = "custom-policy-12345"
		configMapName  = "custom-policy-lua"
		handlerKey     = "handler.lua"
		handlerFromCM  = `return { PRIORITY = 1000, VERSION = "1.0.0" }`
		inlineHandler  = `return { PRIORITY = 1000, VERSION = "2.0.0" }`
	)

	configMapRefValid := func(p *aiconfigurationv1alpha1.AIGatewayCustomPolicy) *metav1.Condition {
		return meta.FindStatusCondition(p.Status.Conditions, konnectv1alpha1.ConfigMapRefValidConditionType)
	}

	w := envtest.SetupWatch[aiconfigurationv1alpha1.AIGatewayCustomPolicyList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating an AIGatewayCustomPolicy whose handler references a ConfigMap that doesn't exist yet")
	policy := &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		Name: "custom-policy", Namespace: ns.Name,
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gateway.Name},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
					Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
						Name:        "my-streaming-policy",
						DisplayName: "My streaming policy",
						Schema: aiconfigurationv1alpha1.ConfigMapDataSource{
							Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
							Value: new("return {}"),
						},
						Handler: aiconfigurationv1alpha1.ConfigMapDataSource{
							Type: aiconfigurationv1alpha1.ConfigMapDataSourceTypeConfigMapRef,
							ConfigMapRef: &aiconfigurationv1alpha1.ConfigMapDataSourceRef{
								Name: configMapName,
								Key:  handlerKey,
							},
						},
					},
				},
			},
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, policy))

	// No SDK expectations are set yet: a Konnect call before the ConfigMap
	// exists fails the test.
	t.Log("Waiting for the ConfigMapRefValid=False and Programmed=False conditions")
	envtest.WatchFor(t, ctx, w, apiwatch.Modified,
		envtest.AssertsAnd(
			envtest.ObjectMatchesName(policy),
			func(p *aiconfigurationv1alpha1.AIGatewayCustomPolicy) bool {
				cond := configMapRefValid(p)
				return cond != nil && cond.Status == metav1.ConditionFalse &&
					envtest.ConditionsContainProgrammedFalse(p.Status.Conditions)
			},
		),
		"AIGatewayCustomPolicy didn't get ConfigMapRefValid=False and Programmed=False",
	)

	t.Log("Setting up SDK expectations on AIGatewayCustomPolicy creation with the handler read from the ConfigMap")
	sdk.AIGatewayCustomPoliciesSDK.EXPECT().
		CreateAiGatewayCustomPolicy(mock.Anything, konnectAIGatewayID, mock.MatchedBy(func(req sdkkonnectcomp.CreateAIGatewayCustomPolicyRequest) bool {
			streaming := req.CreateAIGatewayCustomPolicyStreamingRequest
			return streaming != nil && streaming.Handler == handlerFromCM && streaming.Schema == "return {}"
		})).
		Return(&sdkkonnectops.CreateAiGatewayCustomPolicyResponse{
			AIGatewayCustomPolicy: &sdkkonnectcomp.AIGatewayCustomPolicy{
				AIGatewayCustomPolicyStreaming: &sdkkonnectcomp.AIGatewayCustomPolicyStreaming{
					ID: customPolicyID,
				},
			},
		}, nil)

	t.Log("Creating the referenced ConfigMap")
	require.NoError(t, clientNamespaced.Create(ctx, &corev1.ConfigMap{
		Name:      configMapName,
		Namespace: ns.Name,
		Labels:    map[string]string{"konghq.com/configmap": "true"},
		Data:      map[string]string{handlerKey: handlerFromCM},
	}))

	t.Log("Waiting for the AIGatewayCustomPolicy to be programmed")
	envtest.WatchFor(t, ctx, w, apiwatch.Modified,
		envtest.AssertsAnd(
			envtest.ObjectMatchesName(policy),
			envtest.ObjectMatchesKonnectID[*aiconfigurationv1alpha1.AIGatewayCustomPolicy](customPolicyID),
			envtest.ObjectHasConditionProgrammedSetToTrue[*aiconfigurationv1alpha1.AIGatewayCustomPolicy](),
			func(p *aiconfigurationv1alpha1.AIGatewayCustomPolicy) bool {
				cond := configMapRefValid(p)
				return cond != nil && cond.Status == metav1.ConditionTrue
			},
		),
		"AIGatewayCustomPolicy didn't get programmed after its ConfigMap was created",
	)
	envtest.EventuallyAssertSDKExpectations(t, sdk.AIGatewayCustomPoliciesSDK, consts.WaitTime, consts.TickTime)

	t.Log("Setting up SDK expectations on AIGatewayCustomPolicy update with an inline handler")
	sdk.AIGatewayCustomPoliciesSDK.EXPECT().
		UpdateAiGatewayCustomPolicy(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateAiGatewayCustomPolicyRequest) bool {
			streaming := req.UpdateAIGatewayCustomPolicyRequest.UpdateAIGatewayCustomPolicyStreamingRequest
			return req.GatewayID == konnectAIGatewayID &&
				req.CustomPolicyIDOrName == customPolicyID &&
				streaming != nil && streaming.Handler == inlineHandler
		})).
		Return(&sdkkonnectops.UpdateAiGatewayCustomPolicyResponse{}, nil)

	t.Log("Switching the handler to an inline source")
	require.NoError(t, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(policy), policy))
	policyToPatch := policy.DeepCopy()
	policyToPatch.Spec.APISpec.Streaming.Handler = aiconfigurationv1alpha1.ConfigMapDataSource{
		Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
		Value: new(inlineHandler),
	}
	require.NoError(t, clientNamespaced.Patch(ctx, policyToPatch, client.MergeFrom(policy)))
	policy = policyToPatch

	t.Log("Waiting for the update to be applied and the ConfigMapRefValid condition to be removed")
	envtest.WatchFor(t, ctx, w, apiwatch.Modified,
		envtest.AssertsAnd(
			envtest.ObjectMatchesName(policy),
			envtest.ObjectHasConditionProgrammedSetToTrue[*aiconfigurationv1alpha1.AIGatewayCustomPolicy](),
			func(p *aiconfigurationv1alpha1.AIGatewayCustomPolicy) bool {
				return p.Spec.APISpec.Streaming.Handler.Type == aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline &&
					configMapRefValid(p) == nil
			},
		),
		"AIGatewayCustomPolicy didn't get updated or kept a stale ConfigMapRefValid condition",
	)
	envtest.EventuallyAssertSDKExpectations(t, sdk.AIGatewayCustomPoliciesSDK, consts.WaitTime, consts.TickTime)

	t.Log("Setting up SDK expectations on AIGatewayCustomPolicy deletion")
	sdk.AIGatewayCustomPoliciesSDK.EXPECT().
		DeleteAiGatewayCustomPolicy(mock.Anything, konnectAIGatewayID, customPolicyID).
		Return(&sdkkonnectops.DeleteAiGatewayCustomPolicyResponse{}, nil)

	t.Log("Deleting the AIGatewayCustomPolicy")
	require.NoError(t, clientNamespaced.Delete(ctx, policy))
	eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, policy, consts.WaitTime, consts.TickTime)
	envtest.EventuallyAssertSDKExpectations(t, sdk.AIGatewayCustomPoliciesSDK, consts.WaitTime, consts.TickTime)
}
