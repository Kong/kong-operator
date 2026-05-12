package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayBackendCluster(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.EventGatewayBackendCluster](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.EventGatewayBackendCluster](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and parent KonnectEventGateway")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	gateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)

	const eventGatewayID = "event-gateway-12345"
	updateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, gateway, eventGatewayID)

	t.Run("should create, update and delete EventGatewayBackendCluster successfully", func(t *testing.T) {
		const (
			backendClusterID   = "backend-cluster-12345"
			initialName        = "event-backend-cluster"
			initialDescription = "Backend cluster created from envtest"
			updatedDescription = "Updated backend cluster description"
		)

		w := setupWatch[konnectv1alpha1.EventGatewayBackendClusterList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on EventGatewayBackendCluster creation")
		sdk.EventGatewayBackendClustersSDK.EXPECT().
			CreateEventGatewayBackendCluster(mock.Anything, eventGatewayID, mock.MatchedBy(func(req *sdkkonnectcomp.CreateBackendClusterRequest) bool {
				return req != nil &&
					req.Name == initialName &&
					req.Description != nil && *req.Description == initialDescription &&
					req.Authentication.Type == "anonymous" &&
					len(req.BootstrapServers) == 2 &&
					req.BootstrapServers[0] == "broker-1.example.com:9092" &&
					req.BootstrapServers[1] == "broker-2.example.com:9092" &&
					!req.TLS.Enabled &&
					req.Labels != nil &&
					req.Labels["team"] == "platform" &&
					req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreateEventGatewayBackendClusterResponse{
				BackendCluster: &sdkkonnectcomp.BackendCluster{
					ID: backendClusterID,
				},
			}, nil)

		t.Log("Creating EventGatewayBackendCluster")
		backendCluster := deploy.EventGatewayBackendCluster(t, ctx, clientNamespaced, gateway, func(o client.Object) {
			bc, ok := o.(*konnectv1alpha1.EventGatewayBackendCluster)
			if !ok {
				return
			}
			bc.Spec.APISpec.Name = initialName
			bc.Spec.APISpec.Description = initialDescription
			bc.Spec.APISpec.BootstrapServers = []string{
				"broker-1.example.com:9092",
				"broker-2.example.com:9092",
			}
			bc.Spec.APISpec.Labels = konnectv1alpha1.Labels{
				"team": "platform",
			}
		})

		t.Log("Waiting for EventGatewayBackendCluster to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(backendCluster),
				objectMatchesKonnectID[*konnectv1alpha1.EventGatewayBackendCluster](backendClusterID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.EventGatewayBackendCluster](),
				func(bc *konnectv1alpha1.EventGatewayBackendCluster) bool {
					return bc.GetGatewayID() == eventGatewayID &&
						controllerutil.ContainsFinalizer(bc, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayBackendCluster didn't get Programmed status condition, Konnect ID, parent ID, or cleanup finalizer",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayBackendClustersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayBackendCluster update")
		sdk.EventGatewayBackendClustersSDK.EXPECT().
			UpdateEventGatewayBackendCluster(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayBackendClusterRequest) bool {
				return req.GatewayID == eventGatewayID &&
					req.BackendClusterID == backendClusterID &&
					req.UpdateBackendClusterRequest != nil &&
					req.UpdateBackendClusterRequest.Description != nil &&
					*req.UpdateBackendClusterRequest.Description == updatedDescription &&
					req.UpdateBackendClusterRequest.Labels != nil &&
					req.UpdateBackendClusterRequest.Labels["team"] == "platform" &&
					req.UpdateBackendClusterRequest.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.UpdateEventGatewayBackendClusterResponse{}, nil)

		t.Log("Patching EventGatewayBackendCluster")
		backendClusterToPatch := backendCluster.DeepCopy()
		backendClusterToPatch.Spec.APISpec.Description = updatedDescription
		require.NoError(t, clientNamespaced.Patch(ctx, backendClusterToPatch, client.MergeFrom(backendCluster)))
		backendCluster = backendClusterToPatch

		t.Log("Waiting for EventGatewayBackendCluster to be patched")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(backendCluster),
				objectMatchesKonnectID[*konnectv1alpha1.EventGatewayBackendCluster](backendClusterID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.EventGatewayBackendCluster](),
				func(bc *konnectv1alpha1.EventGatewayBackendCluster) bool {
					return bc.Spec.APISpec.Description == updatedDescription
				},
			),
			"EventGatewayBackendCluster didn't get patched",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayBackendClustersSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on EventGatewayBackendCluster deletion")
		sdk.EventGatewayBackendClustersSDK.EXPECT().
			DeleteEventGatewayBackendCluster(mock.Anything, eventGatewayID, backendClusterID).
			Return(&sdkkonnectops.DeleteEventGatewayBackendClusterResponse{}, nil)

		t.Log("Deleting EventGatewayBackendCluster")
		require.NoError(t, clientNamespaced.Delete(ctx, backendCluster))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, backendCluster, waitTime, tickTime)
		eventuallyAssertSDKExpectations(t, sdk.EventGatewayBackendClustersSDK, waitTime, tickTime)
	})

	t.Run("should create EventGatewayBackendCluster successfully on conflict when backend cluster with matching uid label exists", func(t *testing.T) {
		const backendClusterID = "backend-cluster-conflict-id"

		w := setupWatch[konnectv1alpha1.EventGatewayBackendClusterList](t, ctx, cl, client.InNamespace(ns.Name))

		var backendCluster *konnectv1alpha1.EventGatewayBackendCluster

		sdk.EventGatewayBackendClustersSDK.EXPECT().
			CreateEventGatewayBackendCluster(mock.Anything, eventGatewayID, mock.Anything).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       ErrBodyDataConstraintError,
			})

		sdk.EventGatewayBackendClustersSDK.EXPECT().
			ListEventGatewayBackendClusters(mock.Anything, sdkkonnectops.ListEventGatewayBackendClustersRequest{
				GatewayID: eventGatewayID,
			}).
			RunAndReturn(func(_ context.Context, _ sdkkonnectops.ListEventGatewayBackendClustersRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayBackendClustersResponse, error) {
				return &sdkkonnectops.ListEventGatewayBackendClustersResponse{
					ListBackendClustersResponse: &sdkkonnectcomp.ListBackendClustersResponse{
						Data: []sdkkonnectcomp.BackendCluster{
							{
								ID: backendClusterID,
								Labels: map[string]string{
									ops.KubernetesUIDLabelKey: string(backendCluster.GetUID()),
								},
							},
						},
					},
				}, nil
			})

		t.Log("Creating EventGatewayBackendCluster")
		backendCluster = deploy.EventGatewayBackendCluster(t, ctx, clientNamespaced, gateway)

		t.Log("Waiting for EventGatewayBackendCluster to be programmed after UID conflict lookup")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(backendCluster),
				objectMatchesKonnectID[*konnectv1alpha1.EventGatewayBackendCluster](backendClusterID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.EventGatewayBackendCluster](),
				func(bc *konnectv1alpha1.EventGatewayBackendCluster) bool {
					return bc.GetGatewayID() == eventGatewayID &&
						controllerutil.ContainsFinalizer(bc, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayBackendCluster didn't get Programmed status condition or Konnect ID after conflict resolution",
		)

		eventuallyAssertSDKExpectations(t, sdk.EventGatewayBackendClustersSDK, waitTime, tickTime)
	})
}
