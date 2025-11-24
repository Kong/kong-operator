package envtest

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongTarget(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongTarget](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongTarget](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Run("adding, patching and deleting KongTarget", func(t *testing.T) {
		const (
			upstreamID   = "kup-12345"
			targetID     = "target-12345"
			targetHost   = "example.com"
			targetWeight = 100
		)

		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		w := setupWatch[configurationv1alpha1.KongTargetList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Target creation")
		sdk.TargetsSDK.EXPECT().CreateTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.CreateTargetWithUpstreamRequest) bool {
				return *req.TargetWithoutParents.Target == targetHost && *req.TargetWithoutParents.Weight == int64(targetWeight)
			}),
		).Return(&sdkkonnectops.CreateTargetWithUpstreamResponse{
			Target: &sdkkonnectcomp.Target{
				ID: lo.ToPtr(targetID),
			},
		}, nil)

		t.Log("Creating a KongTarget")
		createdTarget := deploy.KongTargetAttachedToUpstream(t, ctx, clientNamespaced, upstream,
			func(obj client.Object) {
				kt := obj.(*configurationv1alpha1.KongTarget)
				kt.Spec.Target = targetHost
				kt.Spec.Weight = targetWeight
			},
		)

		t.Log("Waiting for Target to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(kt *configurationv1alpha1.KongTarget) bool {
			return kt.GetKonnectID() == targetID && k8sutils.IsProgrammed(kt)
		}, "KongTarget didn't get Programmed status condition or didn't get the correct (target-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.TargetsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Target update")
		sdk.TargetsSDK.EXPECT().UpsertTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertTargetWithUpstreamRequest) bool {
				return req.TargetID == targetID && *req.TargetWithoutParents.Weight == int64(200)
			}),
		).Return(&sdkkonnectops.UpsertTargetWithUpstreamResponse{}, nil)

		t.Log("Patching KongTarget")
		targetToPatch := createdTarget.DeepCopy()
		targetToPatch.Spec.Weight = 200
		require.NoError(t, clientNamespaced.Patch(ctx, targetToPatch, client.MergeFrom(createdTarget)))

		eventuallyAssertSDKExpectations(t, factory.SDK.TargetsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Target deletion")
		sdk.TargetsSDK.EXPECT().DeleteTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.DeleteTargetWithUpstreamRequest) bool {
				return req.TargetID == targetID
			}),
		).Return(&sdkkonnectops.DeleteTargetWithUpstreamResponse{}, nil)

		t.Log("Deleting KongTarget")
		require.NoError(t, clientNamespaced.Delete(ctx, createdTarget))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdTarget, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.TargetsSDK, waitTime, tickTime)
	})

	t.Run("Adopting a target with an upstream", func(t *testing.T) {
		upstreamID := uuid.NewString()
		targetID := uuid.NewString()
		targetHost := "example.com"
		targetWeight := 100

		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		w := setupWatch[configurationv1alpha1.KongTargetList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating targets")
		sdk.TargetsSDK.EXPECT().GetTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.GetTargetWithUpstreamRequest) bool {
				return req.UpstreamIDForTarget == upstreamID && req.TargetID == targetID
			}),
		).Return(&sdkkonnectops.GetTargetWithUpstreamResponse{
			Target: &sdkkonnectcomp.Target{
				ID:     &targetID,
				Weight: lo.ToPtr(int64(targetWeight)),
				Target: &targetHost,
			},
		}, nil)
		sdk.TargetsSDK.EXPECT().UpsertTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertTargetWithUpstreamRequest) bool {
				return req.UpstreamIDForTarget == upstreamID && req.TargetID == targetID
			}),
		).Return(nil, nil)

		t.Logf("Creating a KongTarget to adopt the existing target (ID:%s)", targetID)
		createdTarget := deploy.KongTargetAttachedToUpstream(t, ctx, clientNamespaced, upstream,
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongTarget](commonv1alpha1.AdoptModeOverride, targetID),
		)

		t.Logf("Waiting for KongTarget %s/%s to set Konnect ID and programmed condition", ns.Name, createdTarget.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(kt *configurationv1alpha1.KongTarget) bool {
			return createdTarget.Name == kt.Name &&
				kt.GetKonnectID() == targetID && k8sutils.IsProgrammed(kt)
		},
			fmt.Sprintf("KongTarget didn't get Programmed status condition or didn't get the correct (%s) Konnect ID assigned", targetID),
		)

		t.Log("Setting up SDK expectations for target deletion")
		sdk.TargetsSDK.EXPECT().DeleteTargetWithUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.DeleteTargetWithUpstreamRequest) bool {
				return req.TargetID == targetID && req.UpstreamIDForTarget == upstreamID
			}),
		).Return(nil, nil)

		t.Log("Deleting KongTarget")
		require.NoError(t, clientNamespaced.Delete(ctx, createdTarget))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdTarget, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.TargetsSDK, waitTime, tickTime)
	})
}
