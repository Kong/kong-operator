package envtest

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/modules/manager"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityPluginReconciler[configurationv1alpha1.KongService](false, mgr.GetClient()),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

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

	wKongService := setupWatch[configurationv1alpha1.KongServiceList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
	kongService := deploy.KongServiceAttachedToCP(t, ctx, clientNamespaced, cp)
	kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
		&configurationv1alpha1.KongPluginBinding{
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: cp.Name,
					},
				},
				PluginReference: configurationv1alpha1.PluginRef{
					Name: rateLimitingkongPlugin.Name,
				},
				Targets: configurationv1alpha1.KongPluginBindingTargets{
					ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
						Group: configurationv1alpha1.GroupVersion.Group,
						Kind:  "KongService",
						Name:  kongService.Name,
					},
				},
			},
		},
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
}
