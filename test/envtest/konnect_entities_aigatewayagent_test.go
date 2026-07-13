package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestAIGatewayAgent(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.AIGatewayAgent](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.AIGatewayAgent](&metricsmocks.MockRecorder{}),
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

	const konnectAIGatewayID = "ai-gw-cp-12345"
	updateKonnectAIGatewayStatusWithProgrammed(t, ctx, clientNamespaced, gateway, konnectAIGatewayID)

	t.Run("should create, update and delete AIGatewayAgent successfully", func(t *testing.T) {
		const (
			agentID            = "ai-agent-12345"
			initialDisplayName = "My AI Agent"
			updatedDisplayName = "Updated AI Agent"
			agentURL           = "https://upstream.example.com"
		)

		w := SetupWatch[konnectv1alpha1.AIGatewayAgentList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on AIGatewayAgent creation")
		sdk.AIGatewayAgentsSDK.EXPECT().
			CreateAiGatewayAgent(mock.Anything, konnectAIGatewayID, mock.MatchedBy(func(req sdkkonnectcomp.CreateAIGatewayAgentRequest) bool {
				return req.DisplayName == initialDisplayName &&
					req.Config.URL == agentURL &&
					string(req.Type) == "http" &&
					req.Labels != nil &&
					req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreateAiGatewayAgentResponse{
				AIGatewayAgent: &sdkkonnectcomp.AIGatewayAgent{
					ID: agentID,
				},
			}, nil)

		t.Log("Creating AIGatewayAgent")
		agent := deploy.AIGatewayAgent(t, ctx, clientNamespaced, gateway, func(o client.Object) {
			a, ok := o.(*konnectv1alpha1.AIGatewayAgent)
			if !ok {
				return
			}
			a.Spec.APISpec.DisplayName = initialDisplayName
		})

		t.Log("Waiting for AIGatewayAgent to be programmed")
		WatchFor(t, ctx, w, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(agent),
				ObjectMatchesKonnectID[*konnectv1alpha1.AIGatewayAgent](agentID),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.AIGatewayAgent](),
				func(a *konnectv1alpha1.AIGatewayAgent) bool {
					return a.GetGatewayID() == konnectAIGatewayID &&
						controllerutil.ContainsFinalizer(a, konnect.KonnectCleanupFinalizer)
				},
			),
			"AIGatewayAgent didn't get Programmed status condition, Konnect ID, parent ID, or cleanup finalizer",
		)

		EventuallyAssertSDKExpectations(t, sdk.AIGatewayAgentsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on AIGatewayAgent update")
		sdk.AIGatewayAgentsSDK.EXPECT().
			UpdateAiGatewayAgent(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateAiGatewayAgentRequest) bool {
				return req.GatewayID == konnectAIGatewayID &&
					req.AgentIDOrName == agentID &&
					req.UpdateAIGatewayAgentRequest.DisplayName == updatedDisplayName &&
					req.UpdateAIGatewayAgentRequest.Labels != nil &&
					req.UpdateAIGatewayAgentRequest.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.UpdateAiGatewayAgentResponse{}, nil)

		t.Log("Patching AIGatewayAgent")
		agentToPatch := agent.DeepCopy()
		agentToPatch.Spec.APISpec.DisplayName = updatedDisplayName
		require.NoError(t, clientNamespaced.Patch(ctx, agentToPatch, client.MergeFrom(agent)))
		agent = agentToPatch

		t.Log("Waiting for AIGatewayAgent to be patched")
		WatchFor(t, ctx, w, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(agent),
				ObjectMatchesKonnectID[*konnectv1alpha1.AIGatewayAgent](agentID),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.AIGatewayAgent](),
				func(a *konnectv1alpha1.AIGatewayAgent) bool {
					return a.Spec.APISpec.DisplayName == updatedDisplayName
				},
			),
			"AIGatewayAgent didn't get patched",
		)

		EventuallyAssertSDKExpectations(t, sdk.AIGatewayAgentsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on AIGatewayAgent deletion")
		sdk.AIGatewayAgentsSDK.EXPECT().
			DeleteAiGatewayAgent(mock.Anything, konnectAIGatewayID, agentID).
			Return(&sdkkonnectops.DeleteAiGatewayAgentResponse{}, nil)

		t.Log("Deleting AIGatewayAgent")
		require.NoError(t, clientNamespaced.Delete(ctx, agent))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, agent, waitTime, tickTime)
		EventuallyAssertSDKExpectations(t, sdk.AIGatewayAgentsSDK, waitTime, tickTime)
	})

	t.Run("should resolve ACL references after referenced consumer is programmed", func(t *testing.T) {
		const (
			agentID             = "ai-agent-acl-12345"
			consumerKonnectName = "acl-consumer-konnect-name"
			consumerKonnectID   = "acl-consumer-kid-1"
		)

		t.Log("Creating an unprogrammed AIGatewayConsumer to be referenced by the agent ACL")
		consumer := &konnectv1alpha1.AIGatewayConsumer{
			ObjectMeta: metav1.ObjectMeta{Name: "acl-consumer", Namespace: ns.Name},
			Spec: konnectv1alpha1.AIGatewayConsumerSpec{
				AIGatewayRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gateway.Name},
				},
				APISpec: konnectv1alpha1.AIGatewayConsumerAPISpec{
					Name:        konnectv1alpha1.AIGatewayEntityIdentifier(consumerKonnectName),
					DisplayName: "ACL Consumer",
					Type:        "api-key",
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, consumer))

		w := SetupWatch[konnectv1alpha1.AIGatewayAgentList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating AIGatewayAgent that references the consumer via an ACL allow rule")
		agent := deploy.AIGatewayAgent(t, ctx, clientNamespaced, gateway, func(o client.Object) {
			a, ok := o.(*konnectv1alpha1.AIGatewayAgent)
			if !ok {
				return
			}
			a.Spec.APISpec.Access = konnectv1alpha1.AIGatewayAgentAccess{
				Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
					Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
					Allow: &konnectv1alpha1.AIGatewayAllowACL{
						Allow: []konnectv1alpha1.AIGatewayACLRef{
							{Kind: "AIGatewayConsumer", Name: consumer.Name},
						},
					},
				},
			}
		})

		t.Log("Waiting for KonnectReferencesResolved=False with ReferenceNotProgrammed reason")
		WatchFor(t, ctx, w, apiwatch.Modified,
			func(a *konnectv1alpha1.AIGatewayAgent) bool {
				if a.GetName() != agent.GetName() {
					return false
				}
				cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectReferencesResolvedConditionType, a)
				return ok &&
					cond.Status == metav1.ConditionFalse &&
					cond.Reason == konnectv1alpha1.KonnectReferencesResolvedReasonNotProgrammed
			},
			"AIGatewayAgent didn't report KonnectReferencesResolved=False/ReferenceNotProgrammed for the unprogrammed consumer",
		)

		t.Log("Setting up SDK expectation: after the consumer is programmed, the resolved Konnect name must be pushed")
		sdk.AIGatewayAgentsSDK.EXPECT().
			CreateAiGatewayAgent(mock.Anything, konnectAIGatewayID, mock.MatchedBy(func(req sdkkonnectcomp.CreateAIGatewayAgentRequest) bool {
				return req.Access != nil &&
					req.Access.Acls != nil &&
					req.Access.Acls.AIGatewayAllowACL != nil &&
					len(req.Access.Acls.AIGatewayAllowACL.Allow) == 1 &&
					req.Access.Acls.AIGatewayAllowACL.Allow[0] == consumerKonnectName
			})).
			Return(&sdkkonnectops.CreateAiGatewayAgentResponse{
				AIGatewayAgent: &sdkkonnectcomp.AIGatewayAgent{ID: agentID},
			}, nil)

		t.Log("Programming the referenced consumer by setting its Konnect ID")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(consumer), consumer)) {
				return
			}
			consumer.SetKonnectID(consumerKonnectID)
			assert.NoError(ct, clientNamespaced.Status().Update(ctx, consumer))
		}, waitTime, tickTime)

		t.Log("Waiting for the watch to re-enqueue the agent and flip KonnectReferencesResolved to True")
		WatchFor(t, ctx, w, apiwatch.Modified,
			func(a *konnectv1alpha1.AIGatewayAgent) bool {
				if a.GetName() != agent.GetName() {
					return false
				}
				cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectReferencesResolvedConditionType, a)
				return ok &&
					cond.Status == metav1.ConditionTrue &&
					cond.Reason == konnectv1alpha1.KonnectReferencesResolvedReasonResolved
			},
			"AIGatewayAgent KonnectReferencesResolved didn't flip to True after the consumer was programmed",
		)

		EventuallyAssertSDKExpectations(t, sdk.AIGatewayAgentsSDK, waitTime, tickTime)
	})

	t.Run("should create AIGatewayAgent successfully on conflict when agent with matching uid label exists", func(t *testing.T) {
		const agentID = "ai-agent-conflict-id"

		w := SetupWatch[konnectv1alpha1.AIGatewayAgentList](t, ctx, cl, client.InNamespace(ns.Name))

		var agent *konnectv1alpha1.AIGatewayAgent

		sdk.AIGatewayAgentsSDK.EXPECT().
			CreateAiGatewayAgent(mock.Anything, konnectAIGatewayID, mock.Anything).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       ErrBodyDataConstraintError,
			})

		sdk.AIGatewayAgentsSDK.EXPECT().
			ListAiGatewayAgents(mock.Anything, sdkkonnectops.ListAiGatewayAgentsRequest{
				GatewayID: konnectAIGatewayID,
			}).
			RunAndReturn(func(_ context.Context, _ sdkkonnectops.ListAiGatewayAgentsRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.ListAiGatewayAgentsResponse, error) {
				return &sdkkonnectops.ListAiGatewayAgentsResponse{
					ListAIGatewayAgentsResponse: &sdkkonnectcomp.ListAIGatewayAgentsResponse{
						Data: []sdkkonnectcomp.AIGatewayAgent{
							{
								ID: agentID,
								Labels: map[string]string{
									ops.KubernetesUIDLabelKey: string(agent.GetUID()),
								},
							},
						},
					},
				}, nil
			})

		t.Log("Creating AIGatewayAgent")
		agent = deploy.AIGatewayAgent(t, ctx, clientNamespaced, gateway)

		t.Log("Waiting for AIGatewayAgent to be programmed after UID conflict lookup")
		WatchFor(t, ctx, w, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(agent),
				ObjectMatchesKonnectID[*konnectv1alpha1.AIGatewayAgent](agentID),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.AIGatewayAgent](),
				func(a *konnectv1alpha1.AIGatewayAgent) bool {
					return a.GetGatewayID() == konnectAIGatewayID &&
						controllerutil.ContainsFinalizer(a, konnect.KonnectCleanupFinalizer)
				},
			),
			"AIGatewayAgent didn't get Programmed status condition or Konnect ID after conflict resolution",
		)

		EventuallyAssertSDKExpectations(t, sdk.AIGatewayAgentsSDK, waitTime, tickTime)
	})
}
