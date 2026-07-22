package konnectother

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
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestPortal(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.Portal](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.Portal](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

	t.Run("should create, update and delete Portal successfully", func(t *testing.T) {
		const (
			portalID           = "portal-12345"
			initialDisplayName = "Developer Portal"
			updatedDisplayName = "Updated Developer Portal"
			initialDescription = "Portal created from envtest"
		)

		w := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Portal creation")
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortal) bool {
				return req.DisplayName != nil && *req.DisplayName == initialDisplayName &&
					req.Description != nil && *req.Description == initialDescription &&
					req.Labels != nil &&
					req.Labels["team"] != nil && *req.Labels["team"] == "platform" &&
					req.Labels[ops.KubernetesUIDLabelKey] != nil && *req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalID,
				},
			}, nil)

		t.Log("Creating Portal")
		portal := deploy.Portal(t, ctx, clientNamespaced, apiAuth, func(o client.Object) {
			p, ok := o.(*konnectv1alpha1.Portal)
			if !ok {
				return
			}
			p.Spec.APISpec.DisplayName = initialDisplayName
			p.Spec.APISpec.Description = new(initialDescription)
			p.Spec.APISpec.Labels = konnectv1alpha1.LabelsUpdate{
				"team": "platform",
			}
		})

		t.Log("Waiting for Portal to be programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
				func(p *konnectv1alpha1.Portal) bool {
					return controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"Portal didn't get Programmed status condition, Konnect ID, or cleanup finalizer",
		)

		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on Portal update")
		sdk.PortalsSDK.EXPECT().
			UpdatePortal(mock.Anything, portalID, mock.MatchedBy(func(req sdkkonnectcomp.UpdatePortal) bool {
				return req.DisplayName != nil && *req.DisplayName == updatedDisplayName &&
					req.Labels != nil &&
					req.Labels["team"] != nil && *req.Labels["team"] == "platform" &&
					req.Labels[ops.KubernetesUIDLabelKey] != nil && *req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.UpdatePortalResponse{}, nil)

		t.Log("Patching Portal")
		portalToPatch := portal.DeepCopy()
		portalToPatch.Spec.APISpec.DisplayName = updatedDisplayName
		require.NoError(t, clientNamespaced.Patch(ctx, portalToPatch, client.MergeFrom(portal)))

		t.Log("Waiting for Portal to be patched")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
				func(p *konnectv1alpha1.Portal) bool {
					return p.Spec.APISpec.DisplayName == updatedDisplayName
				},
			),
			"Portal didn't get patched",
		)

		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on Portal deletion")
		sdk.PortalsSDK.EXPECT().
			DeletePortal(mock.Anything, portalID, (*sdkkonnectops.DeletePortalQueryParamForce)(nil)).
			Return(&sdkkonnectops.DeletePortalResponse{}, nil)

		t.Log("Deleting Portal")
		require.NoError(t, clientNamespaced.Delete(ctx, portal))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, portal, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("should create Portal successfully on conflict when Portal with matching uid tag exists", func(t *testing.T) {
		const portalID = "portal-conflict-id"

		w := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))

		// Declared up-front so the ListPortals expectation closure below can
		// read the Portal's UID after k8s assigns it on Create.
		var portal *konnectv1alpha1.Portal

		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.Anything).
			Return(nil, &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       consts.ErrBodyDataConstraintError,
			})

		sdk.PortalsSDK.EXPECT().
			ListPortals(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, _ sdkkonnectops.ListPortalsRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.ListPortalsResponse, error) {
				return &sdkkonnectops.ListPortalsResponse{
					ListPortalsResponse: &sdkkonnectcomp.ListPortalsResponse{
						Data: []sdkkonnectcomp.ListPortalsResponsePortal{
							{
								ID: portalID,
								Labels: map[string]string{
									ops.KubernetesUIDLabelKey: string(portal.GetUID()),
								},
							},
						},
					},
				}, nil
			})

		t.Log("Creating Portal")
		portal = deploy.Portal(t, ctx, clientNamespaced, apiAuth)

		t.Log("Waiting for Portal to be programmed after UID conflict lookup")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID after conflict resolution",
		)

		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)
	})
}
