package envtest

import (
	"context"
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

func TestKongUpstream(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongUpstream](konnectInfiniteSyncTime),
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

	t.Run("adding, patching and deleting KongUpstream", func(t *testing.T) {
		const (
			upstreamID = "upstream-12345"
		)

		t.Log("Setting up a watch for KongUpstream events")
		w := setupWatch[configurationv1alpha1.KongUpstreamList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Upstream creation")
		sdk.UpstreamsSDK.EXPECT().
			CreateUpstream(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.UpstreamInput) bool {
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
		createdUpstream := deploy.KongUpstreamAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongUpstream)
				s.Spec.KongUpstreamAPISpec.Algorithm = sdkkonnectcomp.UpstreamAlgorithmRoundRobin.ToPointer()
			},
		)

		t.Log("Checking SDK KongUpstream operations")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.UpstreamsSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Waiting for Upstream to be programmed and get Konnect ID")
		watchFor(t, ctx, w, watch.Modified, func(r *configurationv1alpha1.KongUpstream) bool {
			return r.GetKonnectID() == upstreamID && k8sutils.IsProgrammed(r)
		}, "KongUpstream didn't get Programmed status condition or didn't get the correct (upstream-12345) Konnect ID assigned")

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

		t.Log("Waiting for Upstream to be updated in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.UpstreamsSDK.AssertExpectations(t))
		}, waitTime, tickTime)

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

		t.Log("Waiting for KongUpstream to disappear")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(createdUpstream), createdUpstream)
			assert.True(c, err != nil && k8serrors.IsNotFound(err))
		}, waitTime, tickTime)

		t.Log("Waiting for Upstream to be deleted in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.UpstreamsSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
