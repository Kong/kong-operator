package konnectother

import (
	"reflect"
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

func TestPortalCustomization(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.PortalCustomization](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.PortalCustomization](&metricsmocks.MockRecorder{}),
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

	t.Run("should create, update and delete PortalCustomization successfully", func(t *testing.T) {
		const (
			portalID      = "portal-12345"
			displayName   = "Developer Portal"
			initialCSS    = "body { background-color: #f0f0f0; }"
			initialLayout = "single-column"
			initialRobots = "User-agent: *\nAllow: /"
			updatedCSS    = "body { background-color: #111111; }"
			updatedLayout = "stacked"
			updatedRobots = "User-agent: *\nDisallow: /private"
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

		customizationWatch := envtest.SetupWatch[konnectv1alpha1.PortalCustomizationList](t, ctx, cl, client.InNamespace(ns.Name))
		customization := testEnvtestPortalCustomization(ns.Name, portal.GetName(), initialCSS, initialLayout, initialRobots)
		expectedCreateRequest, err := customization.Spec.APISpec.ToCreatePortalCustomization()
		require.NoError(t, err)

		sdk.PortalCustomizationSDK.EXPECT().
			ReplacePortalCustomization(
				mock.Anything,
				portalID,
				mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
					return reflect.DeepEqual(req, expectedCreateRequest)
				}),
			).
			Return(&sdkkonnectops.ReplacePortalCustomizationResponse{
				PortalCustomization: &sdkkonnectcomp.PortalCustomization{},
			}, nil)

		t.Log("Creating PortalCustomization")
		require.NoError(t, clientNamespaced.Create(ctx, customization))

		t.Log("Waiting for PortalCustomization to be programmed")
		envtest.WatchFor(t, ctx, customizationWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(customization),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalCustomization](),
				func(p *konnectv1alpha1.PortalCustomization) bool {
					return p.GetPortalID() == portalID &&
						p.GetKonnectID() == "" &&
						p.Spec.APISpec.Css != nil && *p.Spec.APISpec.Css == initialCSS &&
						p.Spec.APISpec.Layout == initialLayout &&
						p.Spec.APISpec.Robots != nil && *p.Spec.APISpec.Robots == initialRobots &&
						controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"PortalCustomization didn't get Programmed status condition, Portal ID, or cleanup finalizer",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalCustomizationSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalCustomization update")
		customizationToPatch := customization.DeepCopy()
		customizationToPatch.Spec.APISpec.Css = new(updatedCSS)
		customizationToPatch.Spec.APISpec.Layout = updatedLayout
		customizationToPatch.Spec.APISpec.Robots = new(updatedRobots)
		expectedUpdateRequest, err := customizationToPatch.Spec.APISpec.ToUpdatePortalCustomization()
		require.NoError(t, err)

		sdk.PortalCustomizationSDK.EXPECT().
			ReplacePortalCustomization(
				mock.Anything,
				portalID,
				mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
					return reflect.DeepEqual(req, expectedUpdateRequest)
				}),
			).
			Return(&sdkkonnectops.ReplacePortalCustomizationResponse{}, nil)

		t.Log("Patching PortalCustomization")
		require.NoError(t, clientNamespaced.Patch(ctx, customizationToPatch, client.MergeFrom(customization)))

		t.Log("Waiting for PortalCustomization to be patched")
		envtest.WatchFor(t, ctx, customizationWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(customization),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalCustomization](),
				func(p *konnectv1alpha1.PortalCustomization) bool {
					return p.GetPortalID() == portalID &&
						p.GetKonnectID() == "" &&
						p.Spec.APISpec.Css != nil && *p.Spec.APISpec.Css == updatedCSS &&
						p.Spec.APISpec.Layout == updatedLayout &&
						p.Spec.APISpec.Robots != nil && *p.Spec.APISpec.Robots == updatedRobots
				},
			),
			"PortalCustomization didn't get patched",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalCustomizationSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalCustomization deletion")
		sdk.PortalCustomizationSDK.EXPECT().
			ReplacePortalCustomization(
				mock.Anything,
				portalID,
				mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
					return reflect.DeepEqual(req, &sdkkonnectcomp.PortalCustomization{})
				}),
			).
			Return(&sdkkonnectops.ReplacePortalCustomizationResponse{}, nil)

		t.Log("Deleting PortalCustomization")
		require.NoError(t, clientNamespaced.Delete(ctx, customization))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, customization, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalCustomizationSDK, consts.WaitTime, consts.TickTime)
	})
}

func testEnvtestPortalCustomization(
	namespace, portalName, css, layout, robots string,
) *konnectv1alpha1.PortalCustomization {
	return &konnectv1alpha1.PortalCustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-customization",
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.PortalCustomizationSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: portalName,
				},
			},
			APISpec: konnectv1alpha1.PortalCustomizationAPISpec{
				Css:    new(css),
				Layout: layout,
				Robots: new(robots),
			},
		},
	}
}
