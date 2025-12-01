package envtest

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
	"github.com/kong/kong-operator/pkg/metadata"
	"github.com/kong/kong-operator/test/helpers/deploy"
)

func TestKongPluginFinalizer(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()

	// Setup up the envtest environment.
	cfg, ns := Setup(t, ctx, scheme.Get())

	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	cl := mgr.GetClient()
	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(cl, ns.Name)

	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityPluginReconciler[configurationv1alpha1.KongService](controller.Options{}, logging.DevelopmentMode, cl),
		konnect.NewKonnectEntityPluginReconciler[configurationv1alpha1.KongRoute](controller.Options{}, logging.DevelopmentMode, cl),
		konnect.NewKonnectEntityPluginReconciler[configurationv1.KongConsumer](controller.Options{}, logging.DevelopmentMode, cl),
		konnect.NewKonnectEntityPluginReconciler[configurationv1beta1.KongConsumerGroup](controller.Options{}, logging.DevelopmentMode, cl),
		konnect.NewKongPluginReconciler(controller.Options{}, logging.DevelopmentMode, cl),
	)

	t.Run("KongService", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, rateLimitingkongPlugin))
		})

		wKongService := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)

		_ = watchFor(t, ctx, wKongService, apiwatch.Modified,
			func(svc *configurationv1alpha1.KongService) bool {
				return svc.Name == kongService.Name &&
					slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongService doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		old := kongService.DeepCopy()
		kongService.Annotations = nil
		require.NoError(t, clientNamespaced.Patch(ctx, kongService, client.MergeFrom(old)))
		_ = watchFor(t, ctx, wKongService, apiwatch.Modified,
			func(svc *configurationv1alpha1.KongService) bool {
				return svc.Name == kongService.Name &&
					!slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongService has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongRoute", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, rateLimitingkongPlugin))
		})

		kongService := deploy.KongService(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		wKongRoute := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongRoute := deploy.KongRoute(
			t, ctx, clientNamespaced,
			deploy.WithNamespacedKongServiceRef(kongService),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)

		_ = watchFor(t, ctx, wKongRoute, apiwatch.Modified,
			func(route *configurationv1alpha1.KongRoute) bool {
				return route.Name == kongRoute.Name &&
					slices.Contains(route.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongRoute doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		old := kongRoute.DeepCopy()
		kongRoute.Annotations = nil
		require.NoError(t, clientNamespaced.Patch(ctx, kongRoute, client.MergeFrom(old)))
		_ = watchFor(t, ctx, wKongRoute, apiwatch.Modified,
			func(route *configurationv1alpha1.KongRoute) bool {
				return route.Name == kongRoute.Name &&
					!slices.Contains(route.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongRoute has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongConsumer", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, rateLimitingkongPlugin))
		})

		wKongConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongConsumer := deploy.KongConsumer(t, ctx, clientNamespaced, "username-1",
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)

		_ = watchFor(t, ctx, wKongConsumer, apiwatch.Modified,
			func(c *configurationv1.KongConsumer) bool {
				return c.Name == kongConsumer.Name &&
					slices.Contains(c.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumer doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		old := kongConsumer.DeepCopy()
		kongConsumer.Annotations = nil
		require.NoError(t, clientNamespaced.Patch(ctx, kongConsumer, client.MergeFrom(old)))
		_ = watchFor(t, ctx, wKongConsumer, apiwatch.Modified,
			func(c *configurationv1.KongConsumer) bool {
				return c.Name == kongConsumer.Name &&
					!slices.Contains(c.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumer has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongConsumerGroup", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)
		t.Cleanup(func() {
			require.NoError(t, clientNamespaced.Delete(ctx, rateLimitingkongPlugin))
		})

		wKongConsumerGroup := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithAnnotation(metadata.AnnotationKeyPlugins, rateLimitingkongPlugin.Name),
		)

		_ = watchFor(t, ctx, wKongConsumerGroup, apiwatch.Modified,
			func(cg *configurationv1beta1.KongConsumerGroup) bool {
				return cg.Name == kongConsumerGroup.Name &&
					slices.Contains(cg.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumerGroup doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		old := kongConsumerGroup.DeepCopy()
		kongConsumerGroup.Annotations = nil
		require.NoError(t, clientNamespaced.Patch(ctx, kongConsumerGroup, client.MergeFrom(old)))
		_ = watchFor(t, ctx, wKongConsumerGroup, apiwatch.Modified,
			func(cg *configurationv1beta1.KongConsumerGroup) bool {
				return cg.Name == kongConsumerGroup.Name &&
					!slices.Contains(cg.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumerGroup has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})
}
