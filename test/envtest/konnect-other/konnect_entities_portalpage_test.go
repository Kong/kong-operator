package konnectother

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
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
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestPortalPage(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []envtest.Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.Portal](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.Portal](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.PortalPage](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.PortalPage](&metricsmocks.MockRecorder{}),
		),
	}
	envtest.StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

	t.Run("should create, update and delete PortalPage successfully", func(t *testing.T) {
		const (
			portalID     = "portal-12345"
			pageID       = "page-12345"
			initialTitle = "Documentation"
			updatedTitle = "Updated documentation"
			initialSlug  = "docs"
			initialBody  = "# docs"
			updatedBody  = "# updated docs"
			description  = "Portal page from envtest"
			displayName  = "Developer Portal"
		)

		portalWatch := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortal) bool {
				return req.DisplayName != nil && *req.DisplayName == displayName &&
					req.Labels != nil &&
					req.Labels[ops.KubernetesUIDLabelKey] != nil && *req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalID,
				},
			}, nil)

		t.Log("Creating Portal")
		portal := deploy.Portal(t, ctx, clientNamespaced, apiAuth, func(obj client.Object) {
			p := obj.(*konnectv1alpha1.Portal)
			p.Spec.APISpec.DisplayName = displayName
		})

		t.Log("Waiting for Portal to be programmed")
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)

		pageWatch := envtest.SetupWatch[konnectv1alpha1.PortalPageList](t, ctx, cl, client.InNamespace(ns.Name))
		page := testEnvtestPortalPage(ns.Name, portal.GetName(), initialTitle, initialSlug, initialBody, description)
		expectedCreateRequest, err := page.Spec.APISpec.ToCreatePortalPageRequest()
		require.NoError(t, err)

		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalID, *expectedCreateRequest).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: pageID,
				},
			}, nil)

		t.Log("Creating PortalPage")
		require.NoError(t, clientNamespaced.Create(ctx, page))

		t.Log("Waiting for PortalPage to be programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(page),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](pageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
				func(p *konnectv1alpha1.PortalPage) bool {
					return p.GetPortalID() == portalID &&
						controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"PortalPage didn't get Programmed status condition, Portal ID, Konnect ID, or cleanup finalizer",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalPage update")
		pageToPatch := page.DeepCopy()
		pageToPatch.Spec.APISpec.Title = konnectv1alpha1.PageTitle(updatedTitle)
		pageToPatch.Spec.APISpec.Content = konnectv1alpha1.PageContent(updatedBody)
		expectedUpdateRequest, err := pageToPatch.Spec.APISpec.ToUpdatePortalPageRequest()
		require.NoError(t, err)

		sdk.PortalPagesSDK.EXPECT().
			UpdatePortalPage(mock.Anything, sdkkonnectops.UpdatePortalPageRequest{
				PortalID:                portalID,
				PageID:                  pageID,
				UpdatePortalPageRequest: *expectedUpdateRequest,
			}).
			Return(&sdkkonnectops.UpdatePortalPageResponse{}, nil)

		t.Log("Patching PortalPage")
		require.NoError(t, clientNamespaced.Patch(ctx, pageToPatch, client.MergeFrom(page)))

		t.Log("Waiting for PortalPage to be patched")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(page),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](pageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
				func(p *konnectv1alpha1.PortalPage) bool {
					return string(p.Spec.APISpec.Title) == updatedTitle &&
						string(p.Spec.APISpec.Content) == updatedBody
				},
			),
			"PortalPage didn't get patched",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalPage deletion")
		sdk.PortalPagesSDK.EXPECT().
			DeletePortalPage(mock.Anything, portalID, pageID).
			Return(&sdkkonnectops.DeletePortalPageResponse{}, nil)

		t.Log("Deleting PortalPage")
		require.NoError(t, clientNamespaced.Delete(ctx, page))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, page, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)
	})
}

func testEnvtestPortalPage(
	namespace, portalName, title, slug, content, description string,
) *konnectv1alpha1.PortalPage {
	return &konnectv1alpha1.PortalPage{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-page",
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.PortalPageSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: portalName,
				},
			},
			APISpec: konnectv1alpha1.PortalPageAPISpec{
				Content:     konnectv1alpha1.PageContent(content),
				Description: konnectv1alpha1.Description(description),
				Slug:        konnectv1alpha1.PageSlug(slug),
				Status:      konnectv1alpha1.PublishedStatus("published"),
				Title:       konnectv1alpha1.PageTitle(title),
				Visibility:  konnectv1alpha1.PageVisibilityStatus("public"),
			},
		},
	}
}
