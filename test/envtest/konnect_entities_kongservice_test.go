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

func TestKongService(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongService](konnectInfiniteSyncTime),
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

	t.Run("adding, patching and deleting KongService", func(t *testing.T) {
		const (
			upstreamID = "kup-12345"
			serviceID  = "service-12345"
			host       = "example.com"
			port       = int64(8081)
		)

		t.Log("Creating a KongUpstream and setting it to programmed")
		upstream := deploy.KongUpstreamAttachedToCP(t, ctx, clientNamespaced, cp)
		updateKongUpstreamStatusWithProgrammed(t, ctx, clientNamespaced, upstream, upstreamID, cp.GetKonnectID())

		t.Log("Setting up a watch for KongService events")
		w := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on Service creation")
		sdk.ServicesSDK.EXPECT().
			CreateService(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.ServiceInput) bool {
					return req.Host == host
				}),
			).
			Return(
				&sdkkonnectops.CreateServiceResponse{
					Service: &sdkkonnectcomp.Service{
						ID: lo.ToPtr(serviceID),
					},
				},
				nil,
			)

		t.Log("Creating a KongService")
		createdService := deploy.KongServiceAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				s := obj.(*configurationv1alpha1.KongService)
				s.Spec.KongServiceAPISpec.Host = host
			},
		)
		t.Log("Checking SDK KongService operations")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ServicesSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Waiting for Service to be programmed and get Konnect ID")
		watchFor(t, ctx, w, watch.Modified, func(kt *configurationv1alpha1.KongService) bool {
			return kt.GetKonnectID() == serviceID && k8sutils.IsProgrammed(kt)
		}, "KongService didn't get Programmed status condition or didn't get the correct (service-12345) Konnect ID assigned")

		t.Log("Setting up SDK expectations on Service update")
		sdk.ServicesSDK.EXPECT().
			UpsertService(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpsertServiceRequest) bool {
					return req.ServiceID == serviceID && req.Service.Port != nil && *req.Service.Port == port
				}),
			).
			Return(&sdkkonnectops.UpsertServiceResponse{}, nil)

		t.Log("Patching KongService")
		serviceToPatch := createdService.DeepCopy()
		serviceToPatch.Spec.Port = lo.ToPtr(port)
		require.NoError(t, clientNamespaced.Patch(ctx, serviceToPatch, client.MergeFrom(createdService)))

		t.Log("Waiting for Service to be updated in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ServicesSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		t.Log("Setting up SDK expectations on Service deletion")
		sdk.ServicesSDK.EXPECT().
			DeleteService(
				mock.Anything,
				cp.GetKonnectID(),
				serviceID,
			).
			Return(&sdkkonnectops.DeleteServiceResponse{}, nil)

		t.Log("Deleting KongService")
		require.NoError(t, clientNamespaced.Delete(ctx, createdService))

		t.Log("Waiting for KongService to disappear")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(createdService), createdService)
			assert.True(c, err != nil && k8serrors.IsNotFound(err))
		}, waitTime, tickTime)

		t.Log("Waiting for Service to be deleted in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.ServicesSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
