package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongPluginBindingUnmanaged(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()

	// Setup up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clientOptions := client.Options{
		Scheme: scheme.Get(),
	}
	cl, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)

	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
	ns2 := deploy.Namespace(t, ctx, mgr.GetClient())
	clientNamespaced2 := client.NewNamespacedClient(mgr.GetClient(), ns2.Name)

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

	t.Run("binding to KongService", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)

		serviceID := uuid.NewString()
		pluginID := uuid.NewString()

		createCall := sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		defer createCall.Unset()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())

		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongService %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongService),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongService), kongService),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})
	t.Run("binding to KongRoute", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)

		serviceID := uuid.NewString()
		routeID := uuid.NewString()
		pluginID := uuid.NewString()

		createCall := sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		defer createCall.Unset()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(t, ctx, clientNamespaced, deploy.WithNamespacedKongServiceRef(kongService))
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)

		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithRouteTarget(kongRoute.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongRoute %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongRoute),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongRoute), kongRoute),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService and KongRoute", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)

		serviceID := uuid.NewString()
		routeID := uuid.NewString()
		pluginID := uuid.NewString()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(t, ctx, clientNamespaced, deploy.WithNamespacedKongServiceRef(kongService))
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)

		sdk.PluginSDK.EXPECT().
			CreatePlugin(
				mock.Anything,
				cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(pi sdkkonnectcomp.Plugin) bool {
					return pi.Route != nil && pi.Route.ID != nil && *pi.Route.ID == routeID &&
						pi.Service != nil && pi.Service.ID != nil && *pi.Service.ID == serviceID
				})).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithRouteTarget(kongRoute.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongRoute %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongRoute),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongRoute), kongRoute),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService and KongConsumer", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)

		serviceID := uuid.NewString()
		consumerID := uuid.NewString()
		pluginID := uuid.NewString()
		username := "test-user" + uuid.NewString()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Cleanup(func() {
			require.NoError(t, client.IgnoreNotFound(clientNamespaced.Delete(ctx, kongService)))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, username,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		t.Cleanup(func() {
			require.NoError(t, client.IgnoreNotFound(clientNamespaced.Delete(ctx, kongConsumer)))
		})
		updateKongConsumerStatusWithKonnectID(t, ctx, clientNamespaced, kongConsumer, consumerID, cp.GetKonnectStatus().GetKonnectID())

		sdk.PluginSDK.EXPECT().
			CreatePlugin(
				mock.Anything,
				cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(pi sdkkonnectcomp.Plugin) bool {
					return pi.Consumer != nil && pi.Consumer.ID != nil && *pi.Consumer.ID == consumerID &&
						pi.Service != nil && pi.Service.ID != nil && *pi.Service.ID == serviceID
				})).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithConsumerTarget(kongConsumer.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongConsumer %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongConsumer),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongConsumer))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongConsumer), kongConsumer),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService and KongConsumerGroup", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)

		serviceID := uuid.NewString()
		consumerGroupID := uuid.NewString()
		pluginID := uuid.NewString()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)

		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongConsumerGroupStatusWithKonnectID(t, ctx, clientNamespaced, kongConsumerGroup, consumerGroupID, cp.GetKonnectStatus().GetKonnectID())

		sdk.PluginSDK.EXPECT().
			CreatePlugin(
				mock.Anything,
				cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(pi sdkkonnectcomp.Plugin) bool {
					return pi.ConsumerGroup != nil && pi.ConsumerGroup.ID != nil && *pi.ConsumerGroup.ID == consumerGroupID &&
						pi.Service != nil && pi.Service.ID != nil && *pi.Service.ID == serviceID
				})).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithServiceTarget(kongService.Name).
				WithConsumerGroupTarget(kongConsumerGroup.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, the check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongConsumerGroup %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongConsumerGroup),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongConsumerGroup))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongConsumerGroup), kongConsumerGroup),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding globally", func(t *testing.T) {
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)
		pluginID := uuid.NewString()

		sdk.PluginSDK.EXPECT().
			CreatePlugin(
				mock.Anything,
				cp.GetKonnectStatus().GetKonnectID(),
				mock.MatchedBy(func(pi sdkkonnectcomp.Plugin) bool {
					return pi.Consumer == nil && pi.ConsumerGroup == nil && pi.Route == nil && pi.Service == nil
				})).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged global KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPluginBinding wasn't created using Konnect API or its KonnectID wasn't set",
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf("delete the unmanaged KongPluginBinding %s, the check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService with KongPlugin from another namespace with valid KongReferenceGrant", func(t *testing.T) {
		w := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, cl, client.InNamespace(ns.Name))
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced2)

		serviceID := uuid.NewString()
		pluginID := uuid.NewString()

		createCall := sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)
		defer createCall.Unset()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())

		krg := deploy.KongReferenceGrant(t, ctx, clientNamespaced2,
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongPluginBinding",
				Namespace: configurationv1alpha1.Namespace(ns.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:  "KongPlugin",
			}),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced2.Delete(ctx, krg))
		})

		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithPluginRefNamespace(ns2.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)

		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s and create it in Konnect",
			client.ObjectKeyFromObject(kpb),
		)
		assert.EventuallyWithT(t,
			assertCollectObjectExistsAndHasKonnectID(t, ctx, clientNamespaced, kpb, pluginID),
			waitTime, tickTime,
			"KongPlugin wasn't created using Konnect API or its KonnectID wasn't set",
		)

		t.Log("Waiting for KongPluginBinding to get ResolvedRefs condition with status=True")
		watchFor(t, ctx, w, apiwatch.Modified, func(k *configurationv1alpha1.KongPluginBinding) bool {
			if k.GetName() != kpb.GetName() || k.GetNamespace() != kpb.GetNamespace() {
				return false
			}

			return k8sutils.HasConditionTrue(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, k)
		}, "KongPluginBinding didn't get ResolvedRefs status condition set to True")

		deleteCall := sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), pluginID).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)
		defer deleteCall.Unset()

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongService %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongService),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongService), kongService),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService with KongPlugin from another namespace without valid KongReferenceGrant", func(t *testing.T) {
		w := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, cl, client.InNamespace(ns.Name))
		proxyCacheKongPlugin := deploy.ProxyCachePlugin(t, ctx, clientNamespaced2)

		serviceID := uuid.NewString()

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())

		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRefName(proxyCacheKongPlugin.Name).
				WithPluginRefNamespace(ns2.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)
		t.Logf(
			"wait for the controller to pick the new unmanaged KongPluginBinding %s",
			client.ObjectKeyFromObject(kpb),
		)

		t.Log("Waiting for KongPluginBinding to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, w, apiwatch.Modified, func(k *configurationv1alpha1.KongPluginBinding) bool {
			if k.GetName() != kpb.GetName() || k.GetNamespace() != kpb.GetNamespace() {
				return false
			}

			return k8sutils.HasConditionFalse(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, k)
		}, "KongPluginBinding didn't get ResolvedRefs status condition set to False")

		t.Logf("delete the unmanaged KongPluginBinding %s, then check it gets collected",
			client.ObjectKeyFromObject(kpb),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpb), kpb),
			))
		}, waitTime, tickTime, "KongPluginBinding did not get deleted but should have")

		t.Logf(
			"delete the KongService %s and check it gets collected, as there should be no finalizer blocking its deletion",
			client.ObjectKeyFromObject(kongService),
		)
		require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		assert.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.True(c, k8serrors.IsNotFound(
				clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongService), kongService),
			))
		}, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})
}
