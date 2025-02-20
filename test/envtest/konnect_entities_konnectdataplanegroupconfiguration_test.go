package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectDataPlaneGroupConfiguration(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](konnectInfiniteSyncTime),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Run("adding and deleting", func(t *testing.T) {
		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth,
			deploy.KonnectGatewayControlPlaneTypeWithCloudGatewaysEnabled(),
		)

		const (
			id = "12345"
		)

		t.Log("Setting up a watch for KonnectCloudGatewayDataPlaneGroupConfiguration events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Setting up SDK expectations on creation")
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID()
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		t.Log("Creating a KonnectCloudGatewayDataPlaneGroupConfiguration")
		dpgconf := deploy.KonnectCloudGatewayDataPlaneGroupConfiguration(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KonnectCloudGatewayDataPlaneGroupConfiguration to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration) bool {
			return c.GetKonnectID() == id && k8sutils.IsProgrammed(c)
		}, "KonnectCloudGatewayDataPlaneGroupConfiguration didn't get Programmed status condition or didn't get the correct (12345) Konnect ID assigned")

		t.Log("Checking SDK operations")
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.CloudGatewaysSDK.AssertExpectations(t))
		}, waitTime, tickTime)

		// NOTE: we delete the data plane group configuration by "creating" (using PUT)
		// because Cloud Gateways DataPlane Group connfiguration API doesn't support
		// deletion of the configuration directly.
		t.Log("Setting up SDK expectations on deletion")
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					len(req.DataplaneGroups) == 0
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		t.Log("Deleting")
		require.NoError(t, clientNamespaced.Delete(ctx, dpgconf))

		t.Log("Waiting for object to disappear")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			err := clientNamespaced.Get(ctx, client.ObjectKeyFromObject(dpgconf), dpgconf)
			assert.True(c, err != nil && k8serrors.IsNotFound(err))
		}, waitTime, tickTime)

		t.Log("Waiting for object to be deleted in the SDK")
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, factory.SDK.CloudGatewaysSDK.AssertExpectations(t))
		}, waitTime, tickTime)
	})
}
