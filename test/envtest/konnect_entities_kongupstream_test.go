package envtest

import (
	"fmt"
	"slices"
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

func TestKongUpstream(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongUpstream](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongUpstream](&metricsmocks.MockRecorder{}),
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

	w := setupWatch[configurationv1alpha1.KongUpstreamList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Run("adding, patching and deleting KongUpstream", func(t *testing.T) {
		const upstreamID = "upstream-12345"

		t.Log("Setting up SDK expectations on Upstream creation")
		sdk.UpstreamsSDK.EXPECT().
			CreateUpstream(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Upstream) bool {
					return req.Algorithm != nil && *req.Algorithm == "round-robin"
				}),
			).
			Return(
				&sdkkonnectops.CreateUpstreamResponse{
					Upstream: &sdkkonnectcomp.Upstream{
						ID: lo.ToPtr(upstreamID),
					},
				},
				nil,
			)

		t.Log("Creating a KongUpstream")
		createdUpstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongUpstream)
				s.Spec.Algorithm = sdkkonnectcomp.UpstreamAlgorithmRoundRobin.ToPointer()
			},
		)

		t.Log("Waiting for Upstream to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *configurationv1alpha1.KongUpstream) bool {
			return r.GetKonnectID() == upstreamID && k8sutils.IsProgrammed(r)
		}, "KongUpstream didn't get Programmed status condition or didn't get the correct (upstream-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.UpstreamsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Upstream update")
		sdk.UpstreamsSDK.EXPECT().
			UpsertUpstream(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertUpstreamRequest) bool {
					return req.UpstreamID == upstreamID &&
						req.Upstream.HashFallback != nil &&
						*req.Upstream.HashFallback == sdkkonnectcomp.HashFallbackHeader
				}),
			).
			Return(&sdkkonnectops.UpsertUpstreamResponse{}, nil)

		t.Log("Patching KongUpstream")
		upstreamToPatch := createdUpstream.DeepCopy()
		upstreamToPatch.Spec.HashFallback = sdkkonnectcomp.HashFallbackHeader.ToPointer()
		upstreamToPatch.Spec.HashFallbackHeader = lo.ToPtr("X-Hash-Header")
		require.NoError(t, clientNamespaced.Patch(ctx, upstreamToPatch, client.MergeFrom(createdUpstream)))

		eventuallyAssertSDKExpectations(t, factory.SDK.UpstreamsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Upstream deletion")
		sdk.UpstreamsSDK.EXPECT().
			DeleteUpstream(
				mock.Anything,
				cp.GetKonnectID(),
				upstreamID,
			).
			Return(&sdkkonnectops.DeleteUpstreamResponse{}, nil)

		t.Log("Deleting KongUpstream")
		require.NoError(t, clientNamespaced.Delete(ctx, createdUpstream))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdUpstream, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.UpstreamsSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongUpstreamList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Upstream creation")
		sdk.UpstreamsSDK.EXPECT().
			CreateUpstream(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.Upstream) bool {
					return slices.Contains(req.Tags, "test-1")
				}),
			).
			Return(
				&sdkkonnectops.CreateUpstreamResponse{
					Upstream: &sdkkonnectcomp.Upstream{
						ID: lo.ToPtr(id),
					},
				},
				nil,
			)

		t.Log("Creating a KongUpstream")
		created := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongUpstream)
				s.Spec.Tags = append(s.Spec.Tags, "test-1")
			},
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.UpstreamsSDK, waitTime, tickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("KongUpstream didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for Service to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongUpstream didn't get Programmed and/or ControlPlaneRefValid status condition set to False")
	})

	t.Run("Adopting an upstream", func(t *testing.T) {
		upstreamID := uuid.NewString()
		upstreamName := uuid.NewString()[:8] + ".example.test"

		w := setupWatch[configurationv1alpha1.KongUpstreamList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating upstreams")
		sdk.UpstreamsSDK.EXPECT().GetUpstream(
			mock.Anything,
			upstreamID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetUpstreamResponse{
			Upstream: &sdkkonnectcomp.Upstream{
				ID:        &upstreamID,
				Name:      upstreamName,
				Algorithm: sdkkonnectcomp.UpstreamAlgorithmConsistentHashing.ToPointer(),
			},
		}, nil)
		sdk.UpstreamsSDK.EXPECT().UpsertUpstream(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertUpstreamRequest) bool {
				return req.UpstreamID == upstreamID
			}),
		).Return(nil, nil)

		t.Logf("Creating a KongUpstream to adopt existing upstream (ID:%s)", upstreamID)
		createdUpstream := deploy.KongUpstream(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongUpstream](commonv1alpha1.AdoptModeOverride, upstreamID),
		)

		t.Logf("Waiting for KongUpstream %s/%s to set Konnect ID and programmed condition", ns.Name, createdUpstream.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(u *configurationv1alpha1.KongUpstream) bool {
			return createdUpstream.Name == u.Name &&
				u.GetKonnectID() == upstreamID && k8sutils.IsProgrammed(u)
		},
			fmt.Sprintf("KongUpstream didn't get Programmed status condition or didn't get the correct (%s) Konnect ID assigned", upstreamID),
		)

		t.Log("Setting up SDK expectations for upstream deletion")
		sdk.UpstreamsSDK.EXPECT().DeleteUpstream(mock.Anything, cp.GetKonnectID(), upstreamID).Return(nil, nil)

		t.Logf("Deleting KongUpstream %s/%s", ns.Name, createdUpstream.Name)
		require.NoError(t, clientNamespaced.Delete(ctx, createdUpstream))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdUpstream, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.UpstreamsSDK, waitTime, tickTime)

	})
}
