package konnectother

import (
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
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKonnectConfigStore(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectConfigStore](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectConfigStore](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and parent KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	// Deleting a config store that still holds secret entries is the common case:
	// the operator never manages those entries, so they are always written out of
	// band. The delete op must therefore force the deletion, otherwise Konnect
	// rejects it and the cleanup finalizer is never released.
	t.Run("should create and force-delete KonnectConfigStore successfully", func(t *testing.T) {
		const configStoreID = "config-store-12345"

		w := envtest.SetupWatch[konnectv1alpha1.KonnectConfigStoreList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KonnectConfigStore creation")
		sdk.ConfigStoresSDK.EXPECT().
			CreateConfigStore(mock.Anything, cp.GetKonnectID(), mock.Anything).
			Return(&sdkkonnectops.CreateConfigStoreResponse{
				ConfigStore: &sdkkonnectcomp.ConfigStore{
					ID: new(configStoreID),
				},
			}, nil).
			Once()

		t.Log("Creating KonnectConfigStore")
		configStore := deploy.KonnectConfigStore(t, ctx, clientNamespaced, cp)

		t.Log("Waiting for KonnectConfigStore to be programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(configStore),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.KonnectConfigStore](configStoreID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.KonnectConfigStore](),
				func(cs *konnectv1alpha1.KonnectConfigStore) bool {
					return cs.GetControlPlaneID() == cp.GetKonnectID() &&
						controllerutil.ContainsFinalizer(cs, konnect.KonnectCleanupFinalizer)
				},
			),
			"KonnectConfigStore didn't get Programmed status condition, Konnect ID, Control Plane ID, or cleanup finalizer",
		)

		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on KonnectConfigStore deletion")
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ConfigStoreID == configStoreID &&
					req.Force != nil && *req.Force == sdkkonnectops.ForceTrue
			})).
			Return(&sdkkonnectops.DeleteConfigStoreResponse{}, nil).
			Once()

		t.Log("Deleting KonnectConfigStore")
		require.NoError(t, clientNamespaced.Delete(ctx, configStore))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, configStore, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)
	})

	// The config store can already be gone on the Konnect side, e.g. when the
	// parent Control Plane was deleted first. The cleanup finalizer must still be
	// released so the CR does not linger in Terminating.
	t.Run("should release the cleanup finalizer when the config store is already gone from Konnect", func(t *testing.T) {
		const configStoreID = "config-store-already-gone"

		w := envtest.SetupWatch[konnectv1alpha1.KonnectConfigStoreList](t, ctx, cl, client.InNamespace(ns.Name))

		sdk.ConfigStoresSDK.EXPECT().
			CreateConfigStore(mock.Anything, cp.GetKonnectID(), mock.Anything).
			Return(&sdkkonnectops.CreateConfigStoreResponse{
				ConfigStore: &sdkkonnectcomp.ConfigStore{
					ID: new(configStoreID),
				},
			}, nil).
			Once()

		configStore := deploy.KonnectConfigStore(t, ctx, clientNamespaced, cp)

		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(configStore),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.KonnectConfigStore](configStoreID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.KonnectConfigStore](),
			),
			"KonnectConfigStore didn't get Programmed status condition or Konnect ID",
		)

		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on KonnectConfigStore deletion returning 404")
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ConfigStoreID == configStoreID
			})).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 404,
				Message:    "not found",
			}).
			Once()

		t.Log("Deleting KonnectConfigStore")
		require.NoError(t, clientNamespaced.Delete(ctx, configStore))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, configStore, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)
	})
}
