package konnectother

import (
	"sync/atomic"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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

	// Deleting a config store that holds no secret entries succeeds without any
	// force flag: Konnect accepts the delete as-is.
	t.Run("should create and delete KonnectConfigStore successfully", func(t *testing.T) {
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

		// No .Once() here: Konnect deletes are at-least-once (e.g. an operator
		// restart between the Konnect call and the finalizer patch would repeat
		// it), so the test only asserts what is sent, not how many times.
		t.Log("Setting up SDK expectations on KonnectConfigStore deletion")
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ConfigStoreID == configStoreID &&
					req.Force == nil
			})).
			Return(&sdkkonnectops.DeleteConfigStoreResponse{}, nil)

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

		// No .Once() here either, for the same at-least-once reason as above.
		t.Log("Setting up SDK expectations on KonnectConfigStore deletion returning 404")
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ConfigStoreID == configStoreID
			})).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 404,
				Message:    "not found",
			})

		t.Log("Deleting KonnectConfigStore")
		require.NoError(t, clientNamespaced.Delete(ctx, configStore))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, configStore, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)
	})

	// Konnect rejects deleting a config store that still holds secret entries
	// with a 400. The operator must not force-delete the entries (an accidental
	// store deletion would instantly break every SNI referencing them): it keeps
	// the cleanup finalizer, reports a DeletionBlocked condition, and retries
	// until the entries are removed.
	t.Run("should block deletion while the config store holds secret entries", func(t *testing.T) {
		const configStoreID = "config-store-with-entries"

		// blocked gates which DeleteConfigStore expectation matches: while set,
		// Konnect rejects the delete; once cleared (simulating the user removing
		// the entries), the delete succeeds.
		var blocked atomic.Bool
		blocked.Store(true)

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

		t.Log("Setting up SDK expectations on KonnectConfigStore deletion blocked by entries")
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ConfigStoreID == configStoreID && blocked.Load()
			})).
			Return(nil, &sdkkonnecterrs.BadRequestError{
				Status: 400,
				Title:  "Bad Request",
				Detail: "server wording is not part of the contract",
			})
		sdk.ConfigStoresSDK.EXPECT().
			DeleteConfigStore(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteConfigStoreRequest) bool {
				return req.ConfigStoreID == configStoreID && !blocked.Load()
			})).
			Return(&sdkkonnectops.DeleteConfigStoreResponse{}, nil)
		sdk.ConfigStoreSecretsSDK.EXPECT().
			ListConfigStoreSecrets(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListConfigStoreSecretsRequest) bool {
				return req.ConfigStoreID == configStoreID &&
					req.PageSize != nil && *req.PageSize == 1
			})).
			Return(&sdkkonnectops.ListConfigStoreSecretsResponse{
				ListConfigStoreSecretsResponse: &sdkkonnectcomp.ListConfigStoreSecretsResponse{
					Data: []sdkkonnectcomp.ConfigStoreSecret{{Key: new("cert-a")}},
				},
			}, nil)

		t.Log("Deleting KonnectConfigStore while it still holds entries")
		require.NoError(t, clientNamespaced.Delete(ctx, configStore))

		t.Log("Waiting for KonnectConfigStore to report the DeletionBlocked condition")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(cs *konnectv1alpha1.KonnectConfigStore) bool {
			if cs.GetName() != configStore.GetName() {
				return false
			}
			c, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cs)
			if !ok {
				return false
			}
			return c.Status == metav1.ConditionFalse &&
				c.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked &&
				controllerutil.ContainsFinalizer(cs, konnect.KonnectCleanupFinalizer)
		}, "KonnectConfigStore should get the Programmed condition set to status=False with reason DeletionBlocked")

		t.Log("Simulating the user removing the entries: the delete now succeeds and the CR is cleaned up")
		blocked.Store(false)
		// Trigger an immediate reconcile instead of waiting for the blocked-deletion
		// requeue. Retry on conflict: the controller may patch the status concurrently.
		// A concurrent reconcile may also have finished the deletion already; that is
		// the goal state, so tolerate NotFound.
		require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(configStore), configStore); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			if configStore.Annotations == nil {
				configStore.Annotations = make(map[string]string)
			}
			configStore.Annotations["gateway-operator.konghq.com/reconcile-after-secret-removal"] = "true"
			if err := clientNamespaced.Update(ctx, configStore); err != nil {
				if apierrors.IsNotFound(err) {
					// The cached Get above can still return the object
					// after it is gone from the API server, so the
					// Update can still hit NotFound.
					return nil
				}
				return err
			}
			return nil
		}))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, configStore, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoresSDK, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.ConfigStoreSecretsSDK, consts.WaitTime, consts.TickTime)
	})
}
