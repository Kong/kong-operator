package envtest

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

func TestKongPluginFinalizer(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
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

	require.NoError(t, manager.SetupCacheIndicesForKonnectTypes(ctx, mgr, false))

	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityPluginReconciler[configurationv1alpha1.KongService](false, mgr.GetClient()),
		konnect.NewKonnectEntityPluginReconciler[configurationv1alpha1.KongRoute](false, mgr.GetClient()),
		konnect.NewKonnectEntityPluginReconciler[configurationv1.KongConsumer](false, mgr.GetClient()),
		konnect.NewKonnectEntityPluginReconciler[configurationv1beta1.KongConsumerGroup](false, mgr.GetClient()),
	)

	t.Run("KongService", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)

		wKongService := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongService := deploy.KongServiceAttachedToCP(t, ctx, clientNamespaced, cp)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRef(rateLimitingkongPlugin.Name).
				WithServiceTarget(kongService.Name).
				Build(),
		)

		_ = watchFor(t, ctx, wKongService, watch.Modified,
			func(svc *configurationv1alpha1.KongService) bool {
				return svc.Name == kongService.Name &&
					slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongService doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		wKongService = setupWatch[configurationv1alpha1.KongServiceList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		_ = watchFor(t, ctx, wKongService, watch.Modified,
			func(svc *configurationv1alpha1.KongService) bool {
				return svc.Name == kongService.Name &&
					!slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongService has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongRoute", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)

		kongService := deploy.KongServiceAttachedToCP(t, ctx, clientNamespaced, cp)
		wKongRoute := setupWatch[configurationv1alpha1.KongRouteList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongRoute := deploy.KongRouteAttachedToService(t, ctx, clientNamespaced, kongService)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRef(rateLimitingkongPlugin.Name).
				WithRouteTarget(kongRoute.Name).
				Build(),
		)

		_ = watchFor(t, ctx, wKongRoute, watch.Modified,
			func(svc *configurationv1alpha1.KongRoute) bool {
				return svc.Name == kongRoute.Name &&
					slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongRoute doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		wKongRoute = setupWatch[configurationv1alpha1.KongRouteList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		_ = watchFor(t, ctx, wKongRoute, watch.Modified,
			func(svc *configurationv1alpha1.KongRoute) bool {
				return svc.Name == kongRoute.Name &&
					!slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongRoute has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongConsumer", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)

		wKongConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongConsumer := deploy.KongConsumerAttachedToCP(t, ctx, clientNamespaced, "username-1", cp)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRef(rateLimitingkongPlugin.Name).
				WithConsumerTarget(kongConsumer.Name).
				Build(),
		)

		_ = watchFor(t, ctx, wKongConsumer, watch.Modified,
			func(svc *configurationv1.KongConsumer) bool {
				return svc.Name == kongConsumer.Name &&
					slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumer doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		wKongConsumer = setupWatch[configurationv1.KongConsumerList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		_ = watchFor(t, ctx, wKongConsumer, watch.Modified,
			func(svc *configurationv1.KongConsumer) bool {
				return svc.Name == kongConsumer.Name &&
					!slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumer has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})

	t.Run("KongConsumerGroup", func(t *testing.T) {
		rateLimitingkongPlugin := deploy.RateLimitingPlugin(t, ctx, clientNamespaced)

		wKongConsumerGroup := setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		kongConsumerGroup := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced, cp)
		kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
			konnect.NewKongPluginBindingBuilder().
				WithControlPlaneRefKonnectNamespaced(cp.Name).
				WithPluginRef(rateLimitingkongPlugin.Name).
				WithConsumerGroupTarget(kongConsumerGroup.Name).
				Build(),
		)

		_ = watchFor(t, ctx, wKongConsumerGroup, watch.Modified,
			func(svc *configurationv1beta1.KongConsumerGroup) bool {
				return svc.Name == kongConsumerGroup.Name &&
					slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumerGroup doesn't have the %s finalizer set", consts.CleanupPluginBindingFinalizer),
		)

		wKongConsumerGroup = setupWatch[configurationv1beta1.KongConsumerGroupList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
		require.NoError(t, clientNamespaced.Delete(ctx, kpb))
		_ = watchFor(t, ctx, wKongConsumerGroup, watch.Modified,
			func(svc *configurationv1beta1.KongConsumerGroup) bool {
				return svc.Name == kongConsumerGroup.Name &&
					!slices.Contains(svc.GetFinalizers(), consts.CleanupPluginBindingFinalizer)
			},
			fmt.Sprintf("KongConsumerGroup has the %s finalizer set but it shouldn't", consts.CleanupPluginBindingFinalizer),
		)
	})
}
