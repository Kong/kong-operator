package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
	"github.com/kong/kong-operator/test/helpers/generate"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func dataplaneGroup(networkRef commonv1alpha1.ObjectRef) []konnectv1alpha1.KonnectConfigurationDataPlaneGroup {
	return []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
		{
			Provider:   sdkkonnectcomp.ProviderNameAws,
			Region:     "us-west-2",
			NetworkRef: networkRef,
			Autoscale: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscale{
				Type: konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleTypeAutopilot,
				Autopilot: &konnectv1alpha1.ConfigurationDataPlaneGroupAutoscaleAutopilot{
					BaseRps: 10,
				},
			},
		},
	}
}

func TestKonnectDataPlaneGroupConfiguration(t *testing.T) {
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
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Run("konnectID adding and deleting", func(t *testing.T) {
		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth,
			deploy.KonnectGatewayControlPlaneTypeWithCloudGatewaysEnabled(),
		)

		var (
			id        = generate.KonnectID(t)
			networkID = generate.KonnectID(t)
		)

		t.Log("Setting up a watch for KonnectCloudGatewayDataPlaneGroupConfiguration events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Setting up SDK expectations on creation")
		const expectedCPGeo = sdkkonnectcomp.ControlPlaneGeoUs // US is the default used by the Mock SDK, we expect this to be propagated.
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ControlPlaneGeo == expectedCPGeo
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		sdk.CloudGatewaysSDK.EXPECT().GetNetwork(
			mock.Anything,
			networkID,
		).Return(
			&sdkkonnectops.GetNetworkResponse{
				StatusCode: 200,
				Network: &sdkkonnectcomp.Network{
					ID:    networkID,
					State: sdkkonnectcomp.NetworkStateReady,
				},
			},
			nil,
		)

		t.Log("Creating a KonnectCloudGatewayDataPlaneGroupConfiguration")
		dpg := deploy.KonnectCloudGatewayDataPlaneGroupConfiguration(t, ctx, clientNamespaced,
			dataplaneGroup(commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: &networkID,
			}),
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KonnectCloudGatewayDataPlaneGroupConfiguration to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration) bool {
			return c.GetKonnectID() == id && k8sutils.IsProgrammed(c)
		}, "KonnectCloudGatewayDataPlaneGroupConfiguration didn't get Programmed status condition or didn't get the correct Konnect ID assigned")

		t.Log("Checking SDK operations")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)

		// NOTE: we delete the data plane group configuration by "creating" (using PUT)
		// because Cloud Gateways DataPlane Group connfiguration API doesn't support
		// deletion of the configuration directly.
		t.Log("Setting up SDK expectations on deletion")
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ControlPlaneGeo == expectedCPGeo &&
					len(req.DataplaneGroups) == 0
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		t.Log("Deleting")
		require.NoError(t, clientNamespaced.Delete(ctx, dpg))
		eventually.WaitForObjectToNotExist(t, ctx, cl, dpg, waitTime, tickTime)

		t.Log("Waiting for object to be deleted in the SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)
	})

	t.Run("namespacedRef adding and deleting", func(t *testing.T) {
		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth,
			deploy.KonnectGatewayControlPlaneTypeWithCloudGatewaysEnabled(),
		)

		var (
			id        = "dpg-" + uuid.New().String()
			networkID = "network-" + uuid.New().String()
		)

		t.Log("Setting up a watch for KonnectCloudGatewayDataPlaneGroupConfiguration events")
		w := setupWatch[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Setting up SDK expectations on creation")
		const expectedCPGeo = sdkkonnectcomp.ControlPlaneGeoUs // US is the default used by the Mock SDK, we expect this to be propagated.
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ControlPlaneGeo == expectedCPGeo
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		n := deploy.KonnectCloudGatewayNetworkWithProgrammed(t, ctx, clientNamespaced, apiAuth,
			func(obj client.Object) {
				n := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
				n.Status.State = string(sdkkonnectcomp.NetworkStateReady)
			},
			deploy.WithKonnectID(networkID),
		)

		dpg := deploy.KonnectCloudGatewayDataPlaneGroupConfiguration(t, ctx, clientNamespaced,
			dataplaneGroup(commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: n.Name,
				},
			}),
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Log("Waiting for KonnectCloudGatewayDataPlaneGroupConfiguration to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration) bool {
			return c.GetKonnectID() == id && k8sutils.IsProgrammed(c)
		}, "KonnectCloudGatewayDataPlaneGroupConfiguration didn't get Programmed status condition or didn't get the correct Konnect ID assigned")

		t.Log("Checking SDK operations")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)

		// NOTE: we delete the data plane group configuration by "creating" (using PUT)
		// because Cloud Gateways DataPlane Group connfiguration API doesn't support
		// deletion of the configuration directly.
		t.Log("Setting up SDK expectations on deletion")
		sdk.CloudGatewaysSDK.EXPECT().CreateConfiguration(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectcomp.CreateConfigurationRequest) bool {
				return req.ControlPlaneID == cp.GetKonnectID() &&
					req.ControlPlaneGeo == expectedCPGeo &&
					len(req.DataplaneGroups) == 0
			}),
		).Return(&sdkkonnectops.CreateConfigurationResponse{
			ConfigurationManifest: &sdkkonnectcomp.ConfigurationManifest{
				ID:             id,
				ControlPlaneID: cp.GetKonnectID(),
			},
		}, nil)

		t.Log("Deleting")
		require.NoError(t, clientNamespaced.Delete(ctx, dpg))
		eventually.WaitForObjectToNotExist(t, ctx, cl, dpg, waitTime, tickTime)

		t.Log("Waiting for object to be deleted in the SDK")
		eventuallyAssertSDKExpectations(t, factory.SDK.CloudGatewaysSDK, waitTime, tickTime)
	})
}
