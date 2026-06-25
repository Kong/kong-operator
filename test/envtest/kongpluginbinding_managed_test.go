package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/metadata"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
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
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
	clientNamespacedWithWatch := client.NewNamespacedClient(clientWithWatch, ns.Name)

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

	addPluginDeleteExpectation := func() {
		sdk.PluginSDK.EXPECT().
			DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.DeletePluginResponse{
					StatusCode: 200,
				},
				nil,
			)
	}

	deleteKongPluginBinding := func(
		t *testing.T,
		ctx context.Context,
		cl client.Client,
		kpb *configurationv1alpha1.KongPluginBinding,
		obj client.Object,
	) {
		t.Helper()
		key := client.ObjectKeyFromObject(kpb)
		var exists bool
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			err := cl.Get(ctx, key, kpb)
			if apierrors.IsNotFound(err) {
				exists = false
				return
			}
			if !assert.NoError(c, err) {
				return
			}

			exists = true
			assert.NotEmpty(c, kpb.GetKonnectID())
			assert.True(c, controllerutil.ContainsFinalizer(kpb, konnect.KonnectCleanupFinalizer))
		}, waitTime, tickTime, "KongPluginBinding %s was not synced before deletion", key)
		if !exists {
			t.Logf("managed kongPluginBinding %s (bound to %s) was already deleted", key, client.ObjectKeyFromObject(obj))
			return
		}

		t.Logf("delete managed kongPluginBinding %s (bound to %s), then check it gets recreated", client.ObjectKeyFromObject(kpb), client.ObjectKeyFromObject(obj))
		addPluginDeleteExpectation()
		require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, kpb)))
		eventually.WaitForObjectToNotExist(t, ctx, cl, kpb, waitTime, tickTime)
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
	sdk.PluginSDK.EXPECT().UpsertPlugin(
		mock.Anything,
		mock.MatchedBy(func(req sdkkonnectops.UpsertPluginRequest) bool {
			return req.ControlPlaneID == cp.GetKonnectStatus().GetKonnectID() &&
				req.PluginID != "" &&
				req.Plugin.Name == rateLimitingkongPlugin.PluginName
		}),
	).Return(nil, nil).Maybe()

	assertKongPluginContainsFinalizerInUse := func(t *testing.T, expected bool, message string) {
		t.Helper()
		require.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var kp configurationv1.KongPlugin
				if !assert.NoError(c, clientNamespacedWithWatch.Get(ctx, client.ObjectKeyFromObject(rateLimitingkongPlugin), &kp)) {
					return
				}
				assert.Equal(c, expected, controllerutil.ContainsFinalizer(&kp, consts.PluginInUseFinalizer))
			},
			waitTime, tickTime,
			message,
		)
	}

	waitForKongPluginBinding := func(
		t *testing.T,
		message string,
		match func(*configurationv1alpha1.KongPluginBinding) bool,
	) *configurationv1alpha1.KongPluginBinding {
		t.Helper()

		var found *configurationv1alpha1.KongPluginBinding
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var list configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(c, clientNamespacedWithWatch.List(ctx, &list)) {
					return
				}

				found = nil
				for i := range list.Items {
					if match(&list.Items[i]) {
						found = list.Items[i].DeepCopy()
						break
					}
				}
				assert.NotNil(c, found)
			},
			waitTime, tickTime,
			message,
		)
		require.NotNil(t, found)
		return found
	}

	// The stale-cache reconcile (driven by pendingKonnectIDs after any CreatePlugin call) reaches
	// ops.Update which calls UpsertPlugin. Register a single optional expectation here to absorb
	// those calls across all subtests. No subtest has a strict UpsertPlugin expectation, so there
	// is no FIFO ordering conflict.
	sdk.PluginSDK.EXPECT().UpsertPlugin(mock.Anything, mock.Anything).
		Return(&sdkkonnectops.UpsertPluginResponse{}, nil).
		Maybe()

	t.Run("binding to KongService", func(t *testing.T) {
		serviceID := uuid.NewString()

		pluginID := uuid.NewString()
		sdk.PluginSDK.EXPECT().
			CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
			Return(
				&sdkkonnectops.CreatePluginResponse{
					Plugin: &sdkkonnectcomp.Plugin{
						ID: new(pluginID),
					},
				},
				nil,
			)

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, kongService))
		})
		updateKongServiceStatusWithProgrammed(t, ctx, clientNamespaced, kongService, serviceID, cp.GetKonnectStatus().GetKonnectID())

		t.Logf("waiting for KongPluginBinding to be created")
		kongPluginBinding := waitForKongPluginBinding(t, "KongPluginBinding wasn't created",
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				targets := kpb.Spec.Targets
				return targets.ServiceReference != nil &&
					targets.ServiceReference.Name == kongService.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		assertKongPluginContainsFinalizerInUse(t, true, "KongPlugin wasn't updated to get plugin-in-use finalizer added")

		deleteKongPluginBinding(t, ctx, clientNamespaced, kongPluginBinding, kongService)
		kongPluginBinding = waitForKongPluginBinding(t, "KongPluginBinding wasn't recreated",
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				svcRef := kpb.Spec.Targets.ServiceReference
				return svcRef != nil &&
					kpb.Name != kongPluginBinding.Name &&
					svcRef.Name == kongService.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)

		t.Logf(
			"remove annotation from KongService %s and check that managed KongPluginBinding %s gets deleted, "+
				"then check that gateway.konghq.com/plugin-in-use finalizer gets removed from %s KongPlugin",
			client.ObjectKeyFromObject(kongService),
			client.ObjectKeyFromObject(kongPluginBinding),
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)

		addPluginDeleteExpectation()

		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer removed",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		delete(kongService.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongService))
		assertKongPluginContainsFinalizerInUse(t, false, "KongPlugin wasn't updated to get plugin-in-use finalizer removed")

		t.Logf(
			"checking that managed KongPluginBinding %s gets deleted",
			client.ObjectKeyFromObject(kongPluginBinding),
		)
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, apierrors.IsNotFound(
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
		kongPluginBinding := waitForKongPluginBinding(t, "KongPluginBinding wasn't created",
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				rRef := kpb.Spec.Targets.RouteReference
				return rRef != nil &&
					rRef.Name == kongRoute.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
		)
		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer added",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		assertKongPluginContainsFinalizerInUse(t, true, "KongPlugin wasn't updated to get plugin-in-use finalizer added")

		deleteKongPluginBinding(t, ctx, clientNamespaced, kongPluginBinding, kongRoute)
		kongPluginBinding = waitForKongPluginBinding(t, "KongPluginBinding wasn't recreated",
			func(kpb *configurationv1alpha1.KongPluginBinding) bool {
				rRef := kpb.Spec.Targets.RouteReference
				return rRef != nil &&
					kpb.Name != kongPluginBinding.Name &&
					rRef.Name == kongRoute.Name &&
					kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
			},
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)

		t.Logf(
			"remove annotation from KongRoute %s and check that managed KongPluginBinding %s gets deleted, "+
				"then check that gateway.konghq.com/plugin-in-use finalizer gets removed from %s KongPlugin",
			client.ObjectKeyFromObject(kongRoute),
			client.ObjectKeyFromObject(kongPluginBinding),
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)

		addPluginDeleteExpectation()

		t.Logf(
			"checking that managed KongPlugin %s gets plugin-in-use finalizer removed",
			client.ObjectKeyFromObject(rateLimitingkongPlugin),
		)
		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))
		assertKongPluginContainsFinalizerInUse(t, false, "KongPlugin wasn't updated to get plugin-in-use finalizer removed")

		t.Logf(
			"checking that managed KongPluginBinding %s gets deleted",
			client.ObjectKeyFromObject(kongPluginBinding),
		)
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, apierrors.IsNotFound(
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
		assertKongPluginContainsFinalizerInUse(t, true, "KongPlugin wasn't updated to get plugin-in-use finalizer added")

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
		addPluginDeleteExpectation()

		delete(kongRoute.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongRoute))
		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				assert.True(c, apierrors.IsNotFound(
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
				assert.True(c, apierrors.IsNotFound(
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
						ID: new(uuid.NewString()),
					},
				},
				nil,
			).
			Maybe()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
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
		assertKongPluginContainsFinalizerInUse(t, true, "KongPlugin wasn't updated to get plugin-in-use finalizer added")

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
		addPluginDeleteExpectation()

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
				assert.True(c, apierrors.IsNotFound(
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

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(c, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:   rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongConsumerReference: kongConsumer.Name,
					},
				)) {
					return
				}
				if !assert.Len(c, l.Items, 1) {
					return
				}
				targets := l.Items[0].Spec.Targets
				assert.Nil(c, targets.RouteReference)
				assert.Nil(c, targets.ServiceReference)
				if assert.NotNil(c, targets.ConsumerReference) {
					assert.Equal(c, kongConsumer.Name, targets.ConsumerReference.Name)
				}
			}, waitTime, tickTime,
			"KongConsumer bound KongPluginBinding wasn't created",
		)

		t.Logf(
			"remove annotation from KongConsumer %s and check that no KongPluginBinding exists that binds to it",
			client.ObjectKeyFromObject(kongConsumer),
		)

		addPluginDeleteExpectation()

		delete(kongConsumer.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongConsumer))

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(c, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:   rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongConsumerReference: kongConsumer.Name,
					},
				)) {
					return
				}
				assert.Empty(c, l.Items)
			}, waitTime, tickTime,
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
						ID: new(uuid.NewString()),
					},
				},
				nil,
			).
			Maybe()

		wKongPluginBinding := setupWatch[configurationv1alpha1.KongPluginBindingList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
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
		assertKongPluginContainsFinalizerInUse(t, true, "KongPlugin wasn't updated to get plugin-in-use finalizer added")

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
		addPluginDeleteExpectation()

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
				assert.True(c, apierrors.IsNotFound(
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

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(c, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:        rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongConsumerGroupReference: kongConsumerGroup.Name,
					},
				)) {
					return
				}
				if !assert.Len(c, l.Items, 1) {
					return
				}
				targets := l.Items[0].Spec.Targets
				assert.Nil(c, targets.RouteReference)
				assert.Nil(c, targets.ServiceReference)
				if assert.NotNil(c, targets.ConsumerGroupReference) {
					assert.Equal(c, kongConsumerGroup.Name, targets.ConsumerGroupReference.Name)
				}
			}, waitTime, tickTime,
			"KongConsumerGroup bound KongPluginBinding wasn't created",
		)

		t.Logf(
			"remove annotation from KongConsumerGroup %s and check that no KongPluginBinding exists that binds to it",
			client.ObjectKeyFromObject(kongConsumerGroup),
		)

		addPluginDeleteExpectation()

		delete(kongConsumerGroup.Annotations, metadata.AnnotationKeyPlugins)
		require.NoError(t, clientNamespaced.Update(ctx, kongConsumerGroup))

		assert.EventuallyWithT(t,
			func(c *assert.CollectT) {
				var l configurationv1alpha1.KongPluginBindingList
				if !assert.NoError(c, clientNamespaced.List(ctx, &l,
					client.MatchingFields{
						index.IndexFieldKongPluginBindingKongPluginReference:        rateLimitingkongPlugin.Namespace + "/" + rateLimitingkongPlugin.Name,
						index.IndexFieldKongPluginBindingKongConsumerGroupReference: kongConsumerGroup.Name,
					},
				)) {
					return
				}
				assert.Empty(c, l.Items)
			}, waitTime, tickTime,
			"KongConsumerGroup bound KongPluginBinding wasn't deleted",
		)

		eventuallyAssertSDKExpectations(t, sdk.PluginSDK, waitTime, tickTime)
	})
}
