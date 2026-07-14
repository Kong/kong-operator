package envtest

import (
	"reflect"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayVirtualClusterConsumePolicy(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and programmed KonnectEventGateway")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	eventGateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)

	const gatewayID = "gateway-12345"
	updateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, eventGateway, gatewayID)

	t.Run("should create, update and delete EventGatewayVirtualClusterConsumePolicy successfully", func(t *testing.T) {
		const (
			backendClusterID   = "backend-cluster-12345"
			virtualClusterID   = "virtual-cluster-12345"
			consumePolicyID    = "consume-policy-12345"
			initialName        = "add-header-1"
			initialDescription = "consume policy created from envtest"
			initialHeaderValue = "added-value"
			updatedDescription = "consume policy updated from envtest"
			updatedHeaderValue = "updated-value"
		)

		w := SetupWatch[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating EventGatewayBackendCluster and setting its status to programmed")
		backendCluster := deploy.EventGatewayBackendCluster(t, ctx, clientNamespaced, eventGateway, deploy.WithName("backend-cluster-a"))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(backendCluster), backendCluster)) {
				return
			}
			backendCluster.Status.Conditions = []metav1.Condition{programmedCondition(backendCluster.GetGeneration())}
			backendCluster.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
				ID:        backendClusterID,
				ServerURL: sdkmocks.SDKServerURL,
				OrgID:     "org-id",
			}
			backendCluster.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: gatewayID}
			require.NoError(ct, clientNamespaced.Status().Update(ctx, backendCluster))
		}, waitTime, tickTime)

		t.Log("Creating EventGatewayVirtualCluster and setting its status to programmed")
		virtualCluster := deploy.EventGatewayVirtualCluster(t, ctx, clientNamespaced, backendCluster, deploy.WithName("virtual-cluster-a"))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(virtualCluster), virtualCluster)) {
				return
			}
			virtualCluster.Status.Conditions = []metav1.Condition{programmedCondition(virtualCluster.GetGeneration())}
			virtualCluster.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
				ID:        virtualClusterID,
				ServerURL: sdkmocks.SDKServerURL,
				OrgID:     "org-id",
			}
			virtualCluster.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: gatewayID}
			require.NoError(ct, clientNamespaced.Status().Update(ctx, virtualCluster))
		}, waitTime, tickTime)

		policy := testEnvtestEventGatewayVirtualClusterConsumePolicy(
			ns.Name,
			virtualCluster.GetName(),
			initialName,
			initialDescription,
			initialHeaderValue,
		)
		expectedCreateRequest, err := policy.Spec.APISpec.ToCreateEventGatewayVirtualClusterConsumePolicyRequest()
		require.NoError(t, err)
		expectedCreateRequest.GatewayID = gatewayID
		expectedCreateRequest.VirtualClusterID = virtualClusterID

		sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
			CreateEventGatewayVirtualClusterConsumePolicy(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyRequest) bool {
					return reflect.DeepEqual(req, *expectedCreateRequest)
				}),
			).
			Return(&sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyResponse{
				EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{
					ID: consumePolicyID,
				},
			}, nil)

		t.Log("Creating EventGatewayVirtualClusterConsumePolicy")
		require.NoError(t, clientNamespaced.Create(ctx, policy))

		t.Log("Waiting for EventGatewayVirtualClusterConsumePolicy to be programmed")
		WatchFor(t, ctx, w, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(policy),
				ObjectMatchesKonnectID[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](consumePolicyID),
				ObjectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](),
				func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) bool {
					cfg := p.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig
					return p.GetGatewayID() == gatewayID &&
						p.GetVirtualClusterID() == virtualClusterID &&
						cfg != nil &&
						cfg.ModifyHeadersPolicyCreate != nil &&
						cfg.ModifyHeadersPolicyCreate.Name == initialName &&
						cfg.ModifyHeadersPolicyCreate.Description == initialDescription &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set != nil &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set.Value == initialHeaderValue &&
						controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayVirtualClusterConsumePolicy didn't get Programmed status condition, parent IDs, Konnect ID, or cleanup finalizer",
		)
		EventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterConsumePoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualClusterConsumePolicy update")
		policyToPatch := policy.DeepCopy()
		policyToPatch.Spec.APISpec.ModifyHeadersPolicyCreate.Description = updatedDescription
		policyToPatch.Spec.APISpec.ModifyHeadersPolicyCreate.Config.Actions[0].Set.Value = updatedHeaderValue
		expectedUpdateRequest, err := policyToPatch.Spec.APISpec.ToUpdateEventGatewayVirtualClusterConsumePolicyRequest()
		require.NoError(t, err)
		expectedUpdateRequest.GatewayID = gatewayID
		expectedUpdateRequest.VirtualClusterID = virtualClusterID
		expectedUpdateRequest.PolicyID = consumePolicyID

		sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
			UpdateEventGatewayVirtualClusterConsumePolicy(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyRequest) bool {
					return reflect.DeepEqual(req, *expectedUpdateRequest)
				}),
			).
			Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyResponse{}, nil)

		t.Log("Patching EventGatewayVirtualClusterConsumePolicy")
		require.NoError(t, clientNamespaced.Patch(ctx, policyToPatch, client.MergeFrom(policy)))
		policy = policyToPatch

		t.Log("Waiting for EventGatewayVirtualClusterConsumePolicy to be patched")
		WatchFor(t, ctx, w, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(policy),
				ObjectMatchesKonnectID[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](consumePolicyID),
				ObjectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](),
				func(p *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy) bool {
					cfg := p.Spec.APISpec.EventGatewayVirtualClusterConsumePolicyConfig
					return p.GetGatewayID() == gatewayID &&
						p.GetVirtualClusterID() == virtualClusterID &&
						cfg != nil &&
						cfg.ModifyHeadersPolicyCreate != nil &&
						cfg.ModifyHeadersPolicyCreate.Description == updatedDescription &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set != nil &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set.Value == updatedHeaderValue
				},
			),
			"EventGatewayVirtualClusterConsumePolicy didn't get patched",
		)
		EventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterConsumePoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualClusterConsumePolicy deletion")
		sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
			DeleteEventGatewayVirtualClusterConsumePolicy(
				mock.Anything,
				sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyRequest{
					GatewayID:        gatewayID,
					VirtualClusterID: virtualClusterID,
					PolicyID:         consumePolicyID,
				},
			).
			Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil)

		t.Log("Deleting EventGatewayVirtualClusterConsumePolicy")
		require.NoError(t, clientNamespaced.Delete(ctx, policy))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, policy, waitTime, tickTime)
		EventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterConsumePoliciesSDK, waitTime, tickTime)
	})
}

func testEnvtestEventGatewayVirtualClusterConsumePolicy(
	namespace, virtualClusterName, name, description, headerValue string,
) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	return &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virtual-cluster-consume-policy",
			Namespace: namespace,
		},
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: virtualClusterName,
				},
			},
			APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
					Type: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &configurationv1alpha1.EventGatewayModifyHeadersPolicyCreate{
						Name:        name,
						Description: description,
						Labels: configurationv1alpha1.Labels{
							"team": "platform",
						},
						Config: configurationv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
							Actions: []configurationv1alpha1.EventGatewayModifyHeaderAction{
								{
									Op: configurationv1alpha1.EventGatewayModifyHeaderActionTypeSet,
									Set: &configurationv1alpha1.EventGatewayModifyHeaderSetAction{
										Key:   "x-added-header",
										Value: headerValue,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
