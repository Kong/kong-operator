package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
	"github.com/kong/kong-operator/pkg/metadata"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongPluginBindingManaged(t *testing.T) {
	// NOTE: Since this test checks the behavior of creating KongPluginBindings
	// based on annotations/ on objects that can have plugins bound to them,
	// need to delete these at the end of each respective subtest to prevent them
	// from being picked up in other tests and cause reconciler to create/update/delete
	// KongPluginBindings.

	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()

	// Setup up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK

	reconcilers := []Reconciler{
		konnect.NewKongPluginReconciler(controller.Options{}, logging.DevelopmentMode, mgr.GetClient()),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongPluginBinding](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongPluginBinding](&metricsmocks.MockRecorder{}),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	deleteKongPluginBinding := func(
		t *testing.T,
		ctx context.Context,
		cl client.Client,
		kpb *configurationv1alpha1.KongPluginBinding,
		obj client.Object,
	) {
		t.Logf("delete managed kongPluginBinding %s (bound to %s), then check it gets recreated", client.ObjectKeyFromObject(kpb), client.ObjectKeyFromObject(obj))
		require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, kpb)))
	}

	rateLimitingkongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "rate-limiting-kp-",
		},
		PluginName: "rate-limiting",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"minute": 5, "policy": "local"}`),
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, rateLimitingkongPlugin))
	t.Logf("deployed %s KongPlugin (%s) resource", client.ObjectKeyFromObject(rateLimitingkongPlugin), rateLimitingkongPlugin.PluginName)

	t.Run("binding to KongService", func(t *testing.T) {
		serviceID := uuid.NewString()

		pluginID := uuid.NewString()
		sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(pluginID),
					},
				},
				nil,
			)

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())

		t.Logf("waiting for KongPluginBinding to be created")
		kongPluginBinding := watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				targets := kpb.Spec.Targets
				return targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
			"KongPluginBinding wasn't created",
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer added",
		)

		t.Logf("delete managed kongPluginBinding %s, then check it gets recreated", client.ObjectKeyFromObject(kongPluginBinding))
		require.NoError(t, clientNamespaced.Delete(ctx, kongPluginBinding))
		kongPluginBinding = watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				svcRef := kpb.Spec.Targets.ServiceReference
				return svcRef != nil &&
					svcRef.Name == kongService.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
			"KongPluginBinding wasn't recreated",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)

		t.Logf(
			"remove annotation from KongService %s and check that managed KongPluginBinding %s gets deleted, "+
				"then check that gateway.konghq.com/plugin-in-use finalizer gets removed from %s KongPlugin",
			client.ObjectKeyFromObject(kongService),
			client.ObjectKeyFromObject(kongPluginBinding),
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer removed",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		wKongPlugin = setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		delete(kongService.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongService))
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					!controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer removed",
		)

		t.Logf(
			"checking that managed KongPluginBinding %s gets deleted",
			client.ObjectKeyFromObject(kongPluginBinding),
		)
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongPluginBinding), kongPluginBinding),
				))
			}, waitTime, tickTime,
			"KongPluginBinding wasn't deleted after removing annotation from KongService",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongRoute", func(t *testing.T) {
		serviceID := uuid.NewString()
		routeID := uuid.NewString()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
			deploy.WithNamespacedKongServiceRef(kongService),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		})
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)

		t.Logf("waiting for KongPluginBinding to be created")
		kongPluginBinding := watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				rRef := kpb.Spec.Targets.RouteReference
				return rRef != nil &&
					rRef.Name == kongRoute.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
			"KongPluginBinding wasn't created",
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer added",
		)

		t.Logf("delete managed kongPluginBinding %s, then check it gets recreated", client.ObjectKeyFromObject(kongPluginBinding))
		require.NoError(t, clientNamespaced.Delete(ctx, kongPluginBinding))
		kongPluginBinding = watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				rRef := kpb.Spec.Targets.RouteReference
				return rRef != nil &&
					rRef.Name == kongRoute.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
			"KongPluginBinding wasn't recreated",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)

		t.Logf(
			"remove annotation from KongRoute %s and check that managed KongPluginBinding %s gets deleted, "+
				"then check that gateway.konghq.com/plugin-in-use finalizer gets removed from %s KongPlugin",
			client.ObjectKeyFromObject(kongRoute),
			client.ObjectKeyFromObject(kongPluginBinding),
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer removed",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					!controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer removed",
		)

		t.Logf(
			"checking that managed KongPluginBinding %s gets deleted",
			client.ObjectKeyFromObject(kongPluginBinding),
		)
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongPluginBinding), kongPluginBinding),
				))
			}, waitTime, tickTime,
			"KongPluginBinding wasn't deleted after removing annotation from KongRoute",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongService and KongRoute", func(t *testing.T) {
		serviceID := uuid.NewString()
		routeID := uuid.NewString()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(kongService),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		})
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)

		t.Logf("waiting for 2 KongPluginBindings to be created")
		var kpbRoute, kpbService *configurationv1alpha1.KongPluginBinding
		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not created",
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer added",
		)

		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbRoute, kongRoute)
		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbService, kongService)

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not recreated",
		)

		t.Logf(
			"remove annotation from KongRoute %s and check that there exists "+
				"a managed KongPluginBinding with only KongService in its targets",
			client.ObjectKeyFromObject(kongRoute),
		)
		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpbRoute), kpbRoute),
				))
			}, waitTime, tickTime,
			"KongPluginBinding bound to Route wasn't deleted after removing annotation from KongRoute",
		)

		t.Logf(
			"remove annotation from KongService %s and check a managed KongPluginBinding (%s) gets deleted",
			client.ObjectKeyFromObject(kongService),
			client.ObjectKeyFromObject(kpbService),
		)

		delete(kongService.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongService))

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpbService), kpbService),
				))
			}, waitTime, tickTime,
			"KongPluginBinding bound to Service wasn't deleted after removing annotation from KongService",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongConsumer, KongService and KongRoute", func(t *testing.T) {
		serviceID := uuid.NewString()
		routeID := uuid.NewString()
		consumerID := uuid.NewString()

		// Permissive CreatePlugin expectation: allow multiple invocations from concurrent reconciles.
		sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(uuid.NewString()),
					},
				},
				nil,
			).
			Maybe()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(kongService),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		})
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)
		kongConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, "username-1",
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongConsumer))
		})
		updateKongConsumerStatusWithKonnectID(t, ctx, clientNamespaced, kongConsumer, consumerID, cp.GetKonnectStatus().GetKonnectID())

		t.Logf("waiting for 2 KongPluginBindings to be created")
		var kpbRoute, kpbService *configurationv1alpha1.KongPluginBinding
		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not created",
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer added",
		)

		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbRoute, kongRoute)
		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbService, kongService)

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not recreated",
		)

		t.Logf(
			"remove annotation from KongRoute %s and check that there exists "+
				"a managed KongPluginBinding with KongService and KongConsumer in its targets",
			client.ObjectKeyFromObject(kongRoute),
		)
		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))

		assert.EventuallyWithT(t,
			func(t *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(t, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:   rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongServiceReference:  kongService.Name,
						index.IndexFieldKongPluginBindingKongConsumerReference: kongConsumer.Name,
					},
				)) {
					return
				}
				assert.Len(t, l.Items, 1)
			}, waitTime, tickTime,
			"KongPluginBinding bound to Consumer and Service doesn't exist but it should",
		)

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpbRoute), kpbRoute),
				))
			}, waitTime, tickTime,
			"KongPluginBinding bound to Route wasn't deleted after removing annotation from KongRoute",
		)

		t.Logf(
			"remove annotation from KongService %s and check that there exists (is created)"+
				"a managed KongPluginBinding with only KongConsumer in its targets",
			client.ObjectKeyFromObject(kongService),
		)

		delete(kongService.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongService))

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				return targets.RouteReference == nil &&
					targets.ServiceReference == nil &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name
			},
			"KongConsumer bound KongPluginBinding wasn't created",
		)

		t.Logf(
			"remove annotation from KongConsumer %s and check that no KongPluginBinding exists that binds to it",
			client.ObjectKeyFromObject(kongConsumer),
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		delete(kongConsumer.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongConsumer))

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Deleted,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				return targets.RouteReference == nil &&
					targets.ServiceReference == nil &&
					targets.ConsumerReference != nil &&
					targets.ConsumerReference.Name == kongConsumer.Name
			},
			"KongConsumer bound KongPluginBinding wasn't deleted",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})

	t.Run("binding to KongConsumerGroup, KongService and KongRoute", func(t *testing.T) {
		serviceID := uuid.NewString()
		routeID := uuid.NewString()
		consumerGroupID := uuid.NewString()

		// Permissive CreatePlugin expectation: allow multiple invocations from concurrent reconciles.
		sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: lo.ToPtr(uuid.NewString()),
					},
				},
				nil,
			).
			Maybe()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())
		kongRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(kongService),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongRoute))
		})
		updateKongRouteStatusWithProgrammed(t, ctx, clientNamespaced, kongRoute, routeID, cp.GetKonnectStatus().GetKonnectID(), serviceID)
		kongConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongConsumerGroup))
		})
		updateKongConsumerGroupStatusWithKonnectID(t, ctx, clientNamespaced, kongConsumerGroup, consumerGroupID, cp.GetKonnectStatus().GetKonnectID())

		t.Logf("waiting for 2 KongPluginBindings to be created")
		var kpbRoute, kpbService *configurationv1alpha1.KongPluginBinding
		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not created",
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		_ = watchFor(t, ctx, wKongPlugin, apiwatch.Modified,
			func(kp *configurationv1.KongPlugin) bool {
				return kp.Name == rateLimitingkongPlugin.Name &&
					controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
			},
			"KongPlugin wasn't updated to get plugin-in-use finalizer added",
		)

		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbRoute, kongRoute)
		deleteKongPluginBinding(t, ctx, clientNamespaced, kpbService, kongService)

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				if targets.RouteReference != nil &&
					targets.RouteReference.Name == kongRoute.Name &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name &&
					targets.ServiceReference == nil {
					kpbRoute = kpb
				} else if targets.RouteReference == nil &&
					targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name {
					kpbService = kpb
				}
				return kpbRoute != nil && kpbService != nil
			},
			"2 KongPluginBindings were not recreated",
		)

		t.Logf(
			"remove annotation from KongRoute %s and check that there exists "+
				"a managed KongPluginBinding with KongService and KongConsumerGroup in its targets",
			client.ObjectKeyFromObject(kongRoute),
		)
		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))

		assert.EventuallyWithT(t,
			func(t *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(t, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:        rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongServiceReference:       kongService.Name,
						index.IndexFieldKongPluginBindingKongConsumerGroupReference: kongConsumerGroup.Name,
					},
				)) {
					return
				}
				assert.Len(t, l.Items, 1)
			}, waitTime, tickTime,
			"KongPluginBinding bound to ConsumerGroup and Service doesn't exist but it should",
		)

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, k8serrors.IsNotFound(
					clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kpbRoute), kpbRoute),
				))
			}, waitTime, tickTime,
			"KongPluginBinding bound to Route wasn't deleted after removing annotation from KongRoute",
		)

		t.Logf(
			"remove annotation from KongService %s and check that there exists (is created)"+
				"a managed KongPluginBinding with only KongConsumerGroup in its targets",
			client.ObjectKeyFromObject(kongService),
		)

		delete(kongService.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongService))

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Added,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				return targets.RouteReference == nil &&
					targets.ServiceReference == nil &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name
			},
			"KongConsumerGroup bound KongPluginBinding wasn't created",
		)

		t.Logf(
			"remove annotation from KongConsumerGroup %s and check that no KongPluginBinding exists that binds to it",
			client.ObjectKeyFromObject(kongConsumerGroup),
		)

		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)

		delete(kongConsumerGroup.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongConsumerGroup))

		watchFor(t, ctx, wKongPluginBinding, apiwatch.Deleted,
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				if kpb.Spec.PluginReference.Name != rateLimitingkongPlugin.Name {
					return false
				}

				targets := kpb.Spec.Targets
				return targets.RouteReference == nil &&
					targets.ServiceReference == nil &&
					targets.ConsumerGroupReference != nil &&
					targets.ConsumerGroupReference.Name == kongConsumerGroup.Name
			},
			"KongConsumerGroup bound KongPluginBinding wasn't deleted",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})
}
