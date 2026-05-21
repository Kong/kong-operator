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
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayVirtualClusterProducePolicy(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](&metricsmocks.MockRecorder{}),
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

	t.Run("should create, update and delete EventGatewayVirtualClusterProducePolicy successfully", func(t *testing.T) {
		const (
			backendClusterID   = "backend-cluster-12345"
			virtualClusterID   = "virtual-cluster-12345"
			producePolicyID    = "produce-policy-12345"
			initialName        = "add-header-1"
			initialDescription = "produce policy created from envtest"
			initialHeaderValue = "added-value"
			updatedDescription = "produce policy updated from envtest"
			updatedHeaderValue = "updated-value"
		)

		w := setupWatch[konnectv1alpha1.EventGatewayVirtualClusterProducePolicyList](t, ctx, cl, client.InNamespace(ns.Name))

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
			backendCluster.Status.GatewayID = &konnectv1alpha1.KonnectEntityRef{ID: gatewayID}
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
			virtualCluster.Status.GatewayID = &konnectv1alpha1.KonnectEntityRef{ID: gatewayID}
			require.NoError(ct, clientNamespaced.Status().Update(ctx, virtualCluster))
		}, waitTime, tickTime)

		policy := testEnvtestEventGatewayVirtualClusterProducePolicy(
			ns.Name,
			virtualCluster.GetName(),
			initialName,
			initialDescription,
			initialHeaderValue,
		)
		expectedCreateRequest, err := policy.Spec.APISpec.ToCreateEventGatewayVirtualClusterProducePolicyRequest()
		require.NoError(t, err)
		expectedCreateRequest.GatewayID = gatewayID
		expectedCreateRequest.VirtualClusterID = virtualClusterID

		sdk.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
			CreateEventGatewayVirtualClusterProducePolicy(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyRequest) bool {
					return reflect.DeepEqual(req, *expectedCreateRequest)
				}),
			).
			Return(&sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyResponse{
				EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{
					ID: producePolicyID,
				},
			}, nil)

		t.Log("Creating EventGatewayVirtualClusterProducePolicy")
		require.NoError(t, clientNamespaced.Create(ctx, policy))

		t.Log("Waiting for EventGatewayVirtualClusterProducePolicy to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(policy),
				objectMatchesKonnectID[*konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](producePolicyID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](),
				func(p *konnectv1alpha1.EventGatewayVirtualClusterProducePolicy) bool {
					cfg := p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig
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
			"EventGatewayVirtualClusterProducePolicy didn't get Programmed status condition, parent IDs, Konnect ID, or cleanup finalizer",
		)
		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterProducePoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualClusterProducePolicy update")
		policyToPatch := policy.DeepCopy()
		policyToPatch.Spec.APISpec.ModifyHeadersPolicyCreate.Description = updatedDescription
		policyToPatch.Spec.APISpec.ModifyHeadersPolicyCreate.Config.Actions[0].Set.Value = updatedHeaderValue
		expectedUpdateRequest, err := policyToPatch.Spec.APISpec.ToUpdateEventGatewayVirtualClusterProducePolicyRequest()
		require.NoError(t, err)
		expectedUpdateRequest.GatewayID = gatewayID
		expectedUpdateRequest.VirtualClusterID = virtualClusterID
		expectedUpdateRequest.PolicyID = producePolicyID

		sdk.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
			UpdateEventGatewayVirtualClusterProducePolicy(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyRequest) bool {
					return reflect.DeepEqual(req, *expectedUpdateRequest)
				}),
			).
			Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyResponse{}, nil)

		t.Log("Patching EventGatewayVirtualClusterProducePolicy")
		require.NoError(t, clientNamespaced.Patch(ctx, policyToPatch, client.MergeFrom(policy)))
		policy = policyToPatch

		t.Log("Waiting for EventGatewayVirtualClusterProducePolicy to be patched")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(policy),
				objectMatchesKonnectID[*konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](producePolicyID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.EventGatewayVirtualClusterProducePolicy](),
				func(p *konnectv1alpha1.EventGatewayVirtualClusterProducePolicy) bool {
					cfg := p.Spec.APISpec.EventGatewayVirtualClusterProducePolicyConfig
					return p.GetGatewayID() == gatewayID &&
						p.GetVirtualClusterID() == virtualClusterID &&
						cfg != nil &&
						cfg.ModifyHeadersPolicyCreate != nil &&
						cfg.ModifyHeadersPolicyCreate.Description == updatedDescription &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set != nil &&
						cfg.ModifyHeadersPolicyCreate.Config.Actions[0].Set.Value == updatedHeaderValue
				},
			),
			"EventGatewayVirtualClusterProducePolicy didn't get patched",
		)
		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterProducePoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualClusterProducePolicy deletion")
		sdk.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
			DeleteEventGatewayVirtualClusterProducePolicy(
				mock.Anything,
				sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyRequest{
					GatewayID:        gatewayID,
					VirtualClusterID: virtualClusterID,
					PolicyID:         producePolicyID,
				},
			).
			Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil)

		t.Log("Deleting EventGatewayVirtualClusterProducePolicy")
		require.NoError(t, clientNamespaced.Delete(ctx, policy))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, policy, waitTime, tickTime)
		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClusterProducePoliciesSDK, waitTime, tickTime)
	})
}

func testEnvtestEventGatewayVirtualClusterProducePolicy(
	namespace, virtualClusterName, name, description, headerValue string,
) *konnectv1alpha1.EventGatewayVirtualClusterProducePolicy {
	return &konnectv1alpha1.EventGatewayVirtualClusterProducePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virtual-cluster-produce-policy",
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
			EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: virtualClusterName,
				},
			},
			APISpec: konnectv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
				EventGatewayVirtualClusterProducePolicyConfig: &konnectv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
					Type: konnectv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &konnectv1alpha1.EventGatewayModifyHeadersPolicyCreate{
						Name:        name,
						Description: description,
						Labels: konnectv1alpha1.Labels{
							"team": "platform",
						},
						Config: konnectv1alpha1.EventGatewayModifyHeadersPolicyCreateConfig{
							Actions: []konnectv1alpha1.EventGatewayModifyHeaderAction{
								{
									Op: konnectv1alpha1.EventGatewayModifyHeaderActionTypeSet,
									Set: &konnectv1alpha1.EventGatewayModifyHeaderSetAction{
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
