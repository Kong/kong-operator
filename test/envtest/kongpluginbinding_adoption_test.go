package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKongPluginBindingAdoption(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()

	// Set up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK

	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongPluginBinding](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongPluginBinding](&metricsmocks.MockRecorder{}),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Run("Adopting a globally applied plugin", func(t *testing.T) {
		pluginID := uuid.NewString()
		w := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating plugins")
		sdk.PluginSDK.EXPECT().GetPlugin(
			mock.Anything,
			sdkkonnectops.GetPluginRequest{
				PluginID:       pluginID,
				ControlPlaneID: cp.GetKonnectID(),
			}).Return(
			&sdkkonnectops.GetPluginResponse{
				Plugin: &sdkkonnectcomp.Plugin{
					ID:   lo.ToPtr(pluginID),
					Name: "proxy-cache",
				},
			}, nil)
		sdk.PluginSDK.EXPECT().UpsertPlugin(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertPluginRequest) bool {
				return req.PluginID == pluginID
			}),
		).Return(nil, nil)

		t.Log("Creating a KongPluginBinding and a KongPlugin to adopt the plugin")
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)
		kpbGlobal := deploy.KongPluginBinding(t, ctx, clientNamespaced, konnect.NewKongPluginBindingBuilder().
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			WithPluginRefName(proxyCacheKongPlugin.Name).
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongPluginBinding](commonv1alpha1.AdoptModeOverride, pluginID),
		)

		t.Logf("Waiting for KongPluginBinding %s/%s being Programmed and set Konnect ID", ns.Name, kpbGlobal.Name)
		watchFor(t, ctx, w,
			apiwatch.Modified,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				return kpb.Name == kpbGlobal.Name &&
					k8sutils.IsProgrammed(kpb) &&
					kpb.GetKonnectID() == pluginID
			}, "Did not see KongPluginBinding set Programmed and Konnect ID")

		t.Log("Setting up SDK expectation for plugin deletion")
		sdk.PluginSDK.EXPECT().DeletePlugin(mock.Anything, cp.GetKonnectID(), pluginID).Return(nil, nil)

		t.Logf("Deleting the KongPluginBinding %s/%s", ns.Name, kpbGlobal.Name)
		require.NoError(t, clientNamespaced.Delete(ctx, kpbGlobal))
		eventually.WaitForObjectToNotExist(t, ctx, cl, kpbGlobal, waitTime, tickTime)
	})

	t.Run("Adopting a plugin attached to a service", func(t *testing.T) {
		pluginID := uuid.NewString()
		w := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating a service with ID")
		kongService := deploy.KongServiceWithID(t, ctx, clientNamespaced, deploy.WithKonnectNamespacedRefControlPlaneRef(cp))
		serviceID := kongService.GetKonnectID()

		t.Log("Setting up SDK expectations for getting and updating plugins")
		sdk.PluginSDK.EXPECT().GetPlugin(
			mock.Anything,
			sdkkonnectops.GetPluginRequest{
				PluginID:       pluginID,
				ControlPlaneID: cp.GetKonnectID(),
			}).Return(
			&sdkkonnectops.GetPluginResponse{
				Plugin: &sdkkonnectcomp.Plugin{
					ID:   lo.ToPtr(pluginID),
					Name: "proxy-cache",
					Service: &sdkkonnectcomp.PluginService{
						ID: lo.ToPtr(serviceID),
					},
				},
			}, nil)
		sdk.PluginSDK.EXPECT().UpsertPlugin(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertPluginRequest) bool {
				return req.PluginID == pluginID
			}),
		).Return(nil, nil)

		t.Log("Creating a KongPluginBinding and a KongPlugin to adopt the plugin")
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)
		kpbService := deploy.KongPluginBinding(t, ctx, clientNamespaced, konnect.NewKongPluginBindingBuilder().
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			WithPluginRefName(proxyCacheKongPlugin.Name).
			WithServiceTarget(kongService.Name).
			Build(),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongPluginBinding](commonv1alpha1.AdoptModeOverride, pluginID),
		)

		t.Logf("Waiting for KongPluginBinding %s/%s being Programmed and set Konnect ID", ns.Name, kpbService.Name)
		watchFor(t, ctx, w,
			apiwatch.Modified,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				return kpb.Name == kpbService.Name &&
					k8sutils.IsProgrammed(kpb) &&
					kpb.GetKonnectID() == pluginID
			}, "Did not see KongPluginBinding set Programmed and Konnect ID")

		t.Log("Setting up SDK expectation for plugin deletion")
		sdk.PluginSDK.EXPECT().DeletePlugin(mock.Anything, cp.GetKonnectID(), pluginID).Return(nil, nil)

		t.Logf("Deleting the KongPluginBinding %s/%s", ns.Name, kpbService.Name)
		require.NoError(t, clientNamespaced.Delete(ctx, kpbService))
		eventually.WaitForObjectToNotExist(t, ctx, cl, kpbService, waitTime, tickTime)
	})

	t.Run("Adopting without KongPlugin reference should fail", func(t *testing.T) {
		pluginID := uuid.NewString()
		w := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting plugins")
		sdk.PluginSDK.EXPECT().GetPlugin(
			mock.Anything,
			sdkkonnectops.GetPluginRequest{
				PluginID:       pluginID,
				ControlPlaneID: cp.GetKonnectID(),
			}).Return(
			&sdkkonnectops.GetPluginResponse{
				Plugin: &sdkkonnectcomp.Plugin{
					ID:   lo.ToPtr(pluginID),
					Name: "proxy-cache",
				},
			}, nil)

		t.Log("Creating a KongPluginBinding without the KongPlugin to adopt the plugin")
		kpbGlobal := deploy.KongPluginBinding(t, ctx, clientNamespaced, konnect.NewKongPluginBindingBuilder().
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			WithPluginRefName("non-exist-plugin").
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongPluginBinding](commonv1alpha1.AdoptModeOverride, pluginID),
		)

		t.Logf("Waiting for the KongPluginBinding %s/%s to be marked as not programmed and not adopted", ns.Name, kpbGlobal.Name)
		watchFor(t, ctx, w,
			apiwatch.Modified,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				return kpb.Name == kpbGlobal.Name &&
					conditionsContainProgrammedFalse(kpb.GetConditions()) &&
					k8sutils.HasConditionFalse(konnectv1alpha1.KonnectEntityAdoptedConditionType, kpb)
			},
			"Did not see KongPluginBinding marked as not programmed and not adopted in its conditions.",
		)
	})
}
