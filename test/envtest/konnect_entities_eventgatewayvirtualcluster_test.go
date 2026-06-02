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

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayVirtualCluster(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualCluster](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualCluster](&metricsmocks.MockRecorder{}),
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

	const expectedParentGateway = "gateway-12345"
	updateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, eventGateway, expectedParentGateway)

	t.Run("should create, update and delete EventGatewayVirtualCluster successfully", func(t *testing.T) {
		const (
			virtualClusterID        = "virtual-cluster-12345"
			backendClusterKonnectID = "backend-cluster-konnect-12345"
			backendClusterName      = "backend-cluster-a"
			initialVirtualName      = "payments"
			updatedVirtualName      = "payments-updated"
			initialDescription      = "virtual cluster created from envtest"
			updatedDescription      = "virtual cluster updated from envtest"
			initialDNSLabel         = "payments"
		)

		w := setupWatch[configurationv1alpha1.EventGatewayVirtualClusterList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating EventGatewayBackendCluster and setting its status to programmed")
		backendCluster := deploy.EventGatewayBackendCluster(t, ctx, clientNamespaced, eventGateway, deploy.WithName(backendClusterName))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(backendCluster), backendCluster)) {
				return
			}
			backendCluster.Status.Conditions = []metav1.Condition{programmedCondition(backendCluster.GetGeneration())}
			backendCluster.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
				ID:        backendClusterKonnectID,
				ServerURL: sdkmocks.SDKServerURL,
				OrgID:     "org-id",
			}
			backendCluster.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: expectedParentGateway}
			require.NoError(ct, clientNamespaced.Status().Update(ctx, backendCluster))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualCluster creation")
		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			CreateEventGatewayVirtualCluster(mock.Anything, expectedParentGateway, mock.MatchedBy(func(req *sdkkonnectcomp.CreateVirtualClusterRequest) bool {
				return req != nil &&
					req.Name == initialVirtualName &&
					req.Description != nil && *req.Description == initialDescription &&
					req.DNSLabel == initialDNSLabel &&
					req.Destination.BackendClusterReferenceByID != nil &&
					req.Destination.BackendClusterReferenceByID.GetID() == backendClusterKonnectID &&
					len(req.Authentication) == 1 &&
					req.Labels != nil &&
					req.Labels["team"] == "platform" &&
					req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreateEventGatewayVirtualClusterResponse{
				VirtualCluster: &sdkkonnectcomp.VirtualCluster{
					ID: virtualClusterID,
				},
			}, nil)

		t.Log("Creating EventGatewayVirtualCluster")
		virtualCluster := deploy.EventGatewayVirtualCluster(t, ctx, clientNamespaced, backendCluster, func(o client.Object) {
			vc, ok := o.(*configurationv1alpha1.EventGatewayVirtualCluster)
			if !ok {
				return
			}
			vc.Spec.APISpec.Name = initialVirtualName
			vc.Spec.APISpec.Description = initialDescription
			vc.Spec.APISpec.DNSLabel = initialDNSLabel
			vc.Spec.APISpec.Labels = configurationv1alpha1.Labels{
				"team": "platform",
			}
		})

		t.Log("Waiting for EventGatewayVirtualCluster to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(virtualCluster),
				objectMatchesKonnectID[*configurationv1alpha1.EventGatewayVirtualCluster](virtualClusterID),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayVirtualCluster](),
				func(vc *configurationv1alpha1.EventGatewayVirtualCluster) bool {
					return vc.GetGatewayID() == expectedParentGateway &&
						controllerutil.ContainsFinalizer(vc, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayVirtualCluster didn't get Programmed status condition, parent Gateway ID, Konnect ID, or cleanup finalizer",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClustersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualCluster update")
		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			UpdateEventGatewayVirtualCluster(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayVirtualClusterRequest) bool {
				return req.GatewayID == expectedParentGateway &&
					req.VirtualClusterID == virtualClusterID &&
					req.UpdateVirtualClusterRequest != nil &&
					req.UpdateVirtualClusterRequest.Name == updatedVirtualName &&
					req.UpdateVirtualClusterRequest.Description != nil &&
					*req.UpdateVirtualClusterRequest.Description == updatedDescription &&
					req.UpdateVirtualClusterRequest.Labels != nil &&
					req.UpdateVirtualClusterRequest.Labels["team"] == "platform" &&
					req.UpdateVirtualClusterRequest.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterResponse{}, nil)

		t.Log("Patching EventGatewayVirtualCluster")
		virtualClusterToPatch := virtualCluster.DeepCopy()
		virtualClusterToPatch.Spec.APISpec.Name = updatedVirtualName
		virtualClusterToPatch.Spec.APISpec.Description = updatedDescription
		require.NoError(t, clientNamespaced.Patch(ctx, virtualClusterToPatch, client.MergeFrom(virtualCluster)))

		t.Log("Waiting for EventGatewayVirtualCluster to be patched")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(virtualCluster),
				objectMatchesKonnectID[*configurationv1alpha1.EventGatewayVirtualCluster](virtualClusterID),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayVirtualCluster](),
				func(vc *configurationv1alpha1.EventGatewayVirtualCluster) bool {
					return vc.GetGatewayID() == expectedParentGateway &&
						vc.Spec.APISpec.Name == updatedVirtualName &&
						vc.Spec.APISpec.Description == updatedDescription
				},
			),
			"EventGatewayVirtualCluster didn't get patched",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClustersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayVirtualCluster deletion")
		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			DeleteEventGatewayVirtualCluster(mock.Anything, expectedParentGateway, virtualClusterID).
			Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterResponse{}, nil)

		t.Log("Deleting EventGatewayVirtualCluster")
		require.NoError(t, clientNamespaced.Delete(ctx, virtualCluster))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, virtualCluster, waitTime, tickTime)
		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClustersSDK, waitTime, tickTime)
	})

	t.Run("should create EventGatewayVirtualCluster successfully on conflict when virtual cluster with matching uid tag exists", func(t *testing.T) {
		const (
			virtualClusterID                = "virtual-cluster-conflict-id"
			conflictBackendClusterKonnectID = "backend-cluster-conflict-konnect-id"
		)

		w := setupWatch[configurationv1alpha1.EventGatewayVirtualClusterList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating EventGatewayBackendCluster 'backend-cluster' and setting its status to programmed")
		conflictBackendCluster := deploy.EventGatewayBackendCluster(t, ctx, clientNamespaced, eventGateway, deploy.WithName("backend-cluster"))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, clientNamespaced.Get(ctx, client.ObjectKeyFromObject(conflictBackendCluster), conflictBackendCluster)) {
				return
			}
			conflictBackendCluster.Status.Conditions = []metav1.Condition{programmedCondition(conflictBackendCluster.GetGeneration())}
			conflictBackendCluster.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
				ID:        conflictBackendClusterKonnectID,
				ServerURL: sdkmocks.SDKServerURL,
				OrgID:     "org-id",
			}
			conflictBackendCluster.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: expectedParentGateway}
			assert.NoError(ct, clientNamespaced.Status().Update(ctx, conflictBackendCluster))
		}, waitTime, tickTime)

		var virtualCluster *configurationv1alpha1.EventGatewayVirtualCluster

		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			CreateEventGatewayVirtualCluster(mock.Anything, expectedParentGateway, mock.Anything).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       ErrBodyDataConstraintError,
			})

		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClustersRequest{
				GatewayID: expectedParentGateway,
			}).
			RunAndReturn(func(_ context.Context, _ sdkkonnectops.ListEventGatewayVirtualClustersRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayVirtualClustersResponse, error) {
				return &sdkkonnectops.ListEventGatewayVirtualClustersResponse{
					ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{
						Data: []sdkkonnectcomp.VirtualCluster{
							{
								ID: virtualClusterID,
								Labels: map[string]string{
									ops.KubernetesUIDLabelKey: string(virtualCluster.GetUID()),
								},
							},
						},
					},
				}, nil
			})

		t.Log("Creating EventGatewayVirtualCluster")
		virtualCluster = deploy.EventGatewayVirtualCluster(t, ctx, clientNamespaced, conflictBackendCluster)

		t.Log("Waiting for EventGatewayVirtualCluster to be programmed after UID conflict lookup")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(virtualCluster),
				objectMatchesKonnectID[*configurationv1alpha1.EventGatewayVirtualCluster](virtualClusterID),
				objectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayVirtualCluster](),
				func(vc *configurationv1alpha1.EventGatewayVirtualCluster) bool {
					return vc.GetGatewayID() == expectedParentGateway
				},
			),
			"EventGatewayVirtualCluster didn't get Programmed status condition, parent Gateway ID, or Konnect ID after conflict resolution",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayVirtualClustersSDK, waitTime, tickTime)
	})
}
