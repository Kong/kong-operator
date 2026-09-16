package konnectother

import (
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
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
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
		expectedCreateRequest, err := page.ToCreatePortalPageRequest(ctx, clientNamespaced)
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
		expectedUpdateRequest, err := pageToPatch.ToUpdatePortalPageRequest(ctx, clientNamespaced)
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

	t.Run("should resolve parentPageIDRef to the parent PortalPage Konnect ID", func(t *testing.T) {
		const (
			portalID     = "portal-67890"
			parentPageID = "parent-page-12345"
			childPageID  = "child-page-12345"
		)

		portalWatch := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalID,
				},
			}, nil).Once()

		t.Log("Creating Portal")
		portal := deploy.Portal(t, ctx, clientNamespaced, apiAuth)

		t.Log("Waiting for Portal to be programmed")
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID",
		)

		pageWatch := envtest.SetupWatch[konnectv1alpha1.PortalPageList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating parent PortalPage")
		parentPage := testEnvtestPortalPage(ns.Name, portal.GetName(), "Parent", "parent", "# parent", "parent page")
		parentPage.Name = "parent-page"
		expectedParentCreate, err := parentPage.ToCreatePortalPageRequest(ctx, clientNamespaced)
		require.NoError(t, err)
		require.Nil(t, expectedParentCreate.ParentPageID)
		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalID, *expectedParentCreate).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: parentPageID,
				},
			}, nil)
		require.NoError(t, clientNamespaced.Create(ctx, parentPage))

		t.Log("Waiting for parent PortalPage to be programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(parentPage),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](parentPageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
			),
			"Parent PortalPage didn't get Programmed status condition or Konnect ID",
		)

		t.Log("Creating child PortalPage referencing the parent via parentPageIDRef")
		childPage := testEnvtestPortalPage(ns.Name, portal.GetName(), "Child", "child", "# child", "child page")
		childPage.Name = "child-page"
		childPage.Spec.APISpec.ParentPageIDRef = &commonv1alpha1.ObjectRef{
			Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{
				Name: parentPage.Name,
			},
		}
		// The expected request resolves the reference through the cluster
		// client: parent_page_id must carry the parent's Konnect ID. The
		// manager's cached client may lag behind the watch that observed the
		// parent's Programmed status, so poll until the cache catches up.
		var expectedChildCreate *sdkkonnectcomp.CreatePortalPageRequest
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			req, err := childPage.ToCreatePortalPageRequest(ctx, clientNamespaced)
			if !assert.NoError(c, err) {
				return
			}
			if assert.NotNil(c, req.ParentPageID) {
				assert.Equal(c, parentPageID, *req.ParentPageID)
			}
			expectedChildCreate = req
		}, consts.WaitTime, consts.TickTime)
		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalID, *expectedChildCreate).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: childPageID,
				},
			}, nil)
		require.NoError(t, clientNamespaced.Create(ctx, childPage))

		t.Log("Waiting for child PortalPage to be programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(childPage),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](childPageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
			),
			"Child PortalPage didn't get Programmed status condition or Konnect ID",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("should re-enqueue a child PortalPage created before its parent is programmed", func(t *testing.T) {
		const (
			portalID     = "portal-99999"
			parentPageID = "parent-page-99999"
			childPageID  = "child-page-99999"
		)

		portalWatch := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalID,
				},
			}, nil).Once()

		t.Log("Creating Portal")
		portal := deploy.Portal(t, ctx, clientNamespaced, apiAuth)

		t.Log("Waiting for Portal to be programmed")
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID",
		)

		pageWatch := envtest.SetupWatch[konnectv1alpha1.PortalPageList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating child PortalPage referencing a parent that does not exist yet")
		childPage := testEnvtestPortalPage(ns.Name, portal.GetName(), "Child First", "child-first", "# child first", "child page created before parent")
		childPage.Name = "child-first-page"
		childPage.Spec.APISpec.ParentPageIDRef = &commonv1alpha1.ObjectRef{
			Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{
				Name: "parent-later-page",
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, childPage))

		t.Log("Waiting for KonnectReferencesResolved=False with ReferenceNotFound reason")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			func(p *konnectv1alpha1.PortalPage) bool {
				if p.GetName() != childPage.GetName() {
					return false
				}
				cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectReferencesResolvedConditionType, p)
				return ok &&
					cond.Status == metav1.ConditionFalse &&
					cond.Reason == konnectv1alpha1.KonnectReferencesResolvedReasonNotFound
			},
			"Child PortalPage didn't report KonnectReferencesResolved=False/ReferenceNotFound for the missing parent",
		)

		t.Log("Setting up SDK expectations for the parent and the re-enqueued child")
		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalID, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortalPageRequest) bool {
				return req.ParentPageID == nil && req.Slug == "parent-later"
			})).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: parentPageID,
				},
			}, nil)
		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalID, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortalPageRequest) bool {
				return req.ParentPageID != nil && *req.ParentPageID == parentPageID && req.Slug == "child-first"
			})).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: childPageID,
				},
			}, nil)

		t.Log("Creating the parent PortalPage")
		parentPage := testEnvtestPortalPage(ns.Name, portal.GetName(), "Parent Later", "parent-later", "# parent later", "parent page created after child")
		parentPage.Name = "parent-later-page"
		require.NoError(t, clientNamespaced.Create(ctx, parentPage))

		t.Log("Waiting for parent PortalPage to be programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(parentPage),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](parentPageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
			),
			"Parent PortalPage didn't get Programmed status condition or Konnect ID",
		)

		t.Log("Waiting for the watch to re-enqueue the child and get it programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(childPage),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalPage](childPageID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
			),
			"Child PortalPage wasn't re-enqueued and programmed after its parent became programmed",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("should reject a parentPageIDRef to a PortalPage under a different Portal", func(t *testing.T) {
		const (
			portalAID    = "portal-a-12345"
			portalBID    = "portal-b-12345"
			parentPageID = "parent-page-a-12345"
		)

		portalWatch := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalAID,
				},
			}, nil).Once()
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalBID,
				},
			}, nil).Once()

		t.Log("Creating two Portals")
		portalA := deploy.Portal(t, ctx, clientNamespaced, apiAuth)
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portalA),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal A didn't get Programmed status condition",
		)
		portalB := deploy.Portal(t, ctx, clientNamespaced, apiAuth)
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portalB),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal B didn't get Programmed status condition",
		)

		pageWatch := envtest.SetupWatch[konnectv1alpha1.PortalPageList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating parent PortalPage under Portal A")
		parentPage := testEnvtestPortalPage(ns.Name, portalA.GetName(), "Parent A", "parent-a", "# parent a", "parent page under portal A")
		parentPage.Name = "parent-page-a"
		sdk.PortalPagesSDK.EXPECT().
			CreatePortalPage(mock.Anything, portalAID, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortalPageRequest) bool {
				return req.Slug == "parent-a"
			})).
			Return(&sdkkonnectops.CreatePortalPageResponse{
				PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
					ID: parentPageID,
				},
			}, nil)
		require.NoError(t, clientNamespaced.Create(ctx, parentPage))

		t.Log("Waiting for parent PortalPage to be programmed")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(parentPage),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalPage](),
			),
			"Parent PortalPage didn't get Programmed status condition",
		)

		t.Log("Creating child PortalPage under Portal B referencing the parent under Portal A")
		childPage := testEnvtestPortalPage(ns.Name, portalB.GetName(), "Child B", "child-b", "# child b", "child page under portal B")
		childPage.Name = "child-page-b"
		childPage.Spec.APISpec.ParentPageIDRef = &commonv1alpha1.ObjectRef{
			Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{
				Name: parentPage.Name,
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, childPage))

		t.Log("Waiting for KonnectReferencesResolved=False with ReferenceInvalid reason")
		envtest.WatchFor(t, ctx, pageWatch, apiwatch.Modified,
			func(p *konnectv1alpha1.PortalPage) bool {
				if p.GetName() != childPage.GetName() {
					return false
				}
				cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectReferencesResolvedConditionType, p)
				return ok &&
					cond.Status == metav1.ConditionFalse &&
					cond.Reason == konnectv1alpha1.KonnectReferencesResolvedReasonInvalid
			},
			"Child PortalPage didn't report KonnectReferencesResolved=False/ReferenceInvalid for the cross-Portal parent reference",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalPagesSDK, consts.WaitTime, consts.TickTime)
	})
}

func testEnvtestPortalPage(
	namespace, portalName, title, slug, content, description string,
) *konnectv1alpha1.PortalPage {
	return &konnectv1alpha1.PortalPage{
		Name:      "portal-page",
		Namespace: namespace,
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
