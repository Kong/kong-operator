package envtest

import (
	"slices"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

func TestKongRoute(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongRoute](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongRoute](&metricsmocks.MockRecorder{}),
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
	svc := deploy.KongServiceWithID(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
	)

	t.Run("adding, patching and deleting KongRoute", func(t *testing.T) {
		const routeID = "route-12345"

		w := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Route creation")
		sdk.RoutesSDK.EXPECT().
			CreateRoute(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Route) bool {
					return slices.Equal(req.RouteJSON.Paths, []string{"/path"})
				}),
			).
			Return(
				&sdkkonnectops.CreateRouteResponse{
					Route: &sdkkonnectcomp.Route{
						RouteJSON: &sdkkonnectcomp.RouteJSON{
							ID: lo.ToPtr(routeID),
						},
					},
				},
				nil,
			)

		t.Log("Creating a KongRoute")
		createdRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(svc),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongRoute)
				s.Spec.Paths = []string{"/path"}
			},
		)

		t.Log("Waiting for Route to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			if r.GetName() != createdRoute.GetName() {
				return false
			}
			return r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		}, "KongRoute didn't get Programmed status condition or didn't get the correct (route-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Route update")
		sdk.RoutesSDK.EXPECT().
			UpsertRoute(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertRouteRequest) bool {
					return req.RouteID == routeID &&
						slices.Equal(req.Route.RouteJSON.Paths, []string{"/path"}) &&
						req.Route.RouteJSON.PreserveHost != nil && *req.Route.RouteJSON.PreserveHost == true
				}),
			).
			Return(&sdkkonnectops.UpsertRouteResponse{}, nil)

		t.Log("Patching KongRoute")
		routeToPatch := createdRoute.DeepCopy()
		routeToPatch.Spec.PreserveHost = lo.ToPtr(true)
		require.NoError(t, clientNamespaced.Patch(ctx, routeToPatch, client.MergeFrom(createdRoute)))

		t.Log("Waiting for Route to get the update")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
			if r.GetName() != createdRoute.GetName() {
				return false
			}
			if r.Spec.PreserveHost == nil || !*r.Spec.PreserveHost {
				return false
			}
			return r.GetKonnectID() == routeID && k8sutils.IsProgrammed(r)
		}, "KongRoute didn't get patched with PreserveHost=true")

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Route deletion")
		sdk.RoutesSDK.EXPECT().
			DeleteRoute(
				mock.Anything,
				cp.GetKonnectID(),
				routeID,
			).
			Return(&sdkkonnectops.DeleteRouteResponse{}, nil)

		t.Log("Deleting KongRoute")
		require.NoError(t, clientNamespaced.Delete(ctx, createdRoute))

		eventually.WaitForObjectToNotExist(t, ctx, cl, createdRoute, waitTime, tickTime)
	})
}
