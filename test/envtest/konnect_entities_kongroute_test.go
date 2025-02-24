package envtest

import (
	"slices"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongRoute](konnectInfiniteSyncTime),
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
				mock.MatchedBy(func(req sdkkonnectcomp.RouteInput) bool {
					return slices.Equal(req.RouteJSONInput.Paths, []string{"/path"})
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
		createdRoute := deploy.KongRouteAttachedToService(t, ctx, clientNamespaced, svc,
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongRoute)
				s.Spec.KongRouteAPISpec.Paths = []string{"/path"}
			},
		)

		t.Log("Waiting for Route to be programmed and get Konnect ID")
		watchFor(t, ctx, w, watch.Modified, func(r *configurationv1alpha1.KongRoute) bool {
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
						slices.Equal(req.Route.RouteJSONInput.Paths, []string{"/path"}) &&
						req.Route.RouteJSONInput.PreserveHost != nil && *req.Route.RouteJSONInput.PreserveHost == true
				}),
			).
			Return(&sdkkonnectops.UpsertRouteResponse{}, nil)

		t.Log("Patching KongRoute")
		routeToPatch := createdRoute.DeepCopy()
		routeToPatch.Spec.PreserveHost = lo.ToPtr(true)
		require.NoError(t, clientNamespaced.Patch(ctx, routeToPatch, client.MergeFrom(createdRoute)))

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

		t.Log("Waiting for KongRoute to disappear")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(createdRoute), createdRoute)
			assert.True(c, err != nil && k8serrors.IsNotFound(err))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.RoutesSDK, waitTime, tickTime)
	})
}
