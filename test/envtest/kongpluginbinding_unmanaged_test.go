package envtest

import (
	"context"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongPluginBindingUnmanaged(t *testing.T) {
	const (
		konnectSyncTime = 100 * time.Millisecond
		waitTime        = 20 * time.Second
		tickTime        = 500 * time.Millisecond
	)

	// Setup up the envtest environment and share it across the test cases.
	cfg := Setup(t, scheme.Get())
	t.Parallel()

	ctx, cancel := Context(t, context.Background())
	defer cancel()
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	clientWithWatch, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
		},
	}
	require.NoError(t, clientWithWatch.Create(ctx, ns))
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	apiAuth := deployKonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deployKonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	factory := ops.NewMockSDKFactory(t)
	serviceID := uuid.NewString()
	factory.SDK.ServicesSDK.EXPECT().
		CreateService(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
		Return(
			&sdkkonnectops.CreateServiceResponse{
				Service: &sdkkonnectcomp.Service{
					ID: lo.ToPtr(serviceID),
				},
			},
			nil,
		)
	factory.SDK.ServicesSDK.EXPECT().
		UpsertService(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertServiceResponse{
				Service: &sdkkonnectcomp.Service{
					ID: lo.ToPtr(serviceID),
				},
			},
			nil,
		)

	reconcilers := []Reconciler{
		konnect.NewKongPluginReconciler(false, mgr.GetClient()),
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongPluginBinding](konnectSyncTime),
		),
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongService](konnectSyncTime),
		),
	}

	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	proxyCacheKongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "proxy-cache-kp-",
		},
		PluginName: "proxy-cache",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"response_code": [200], "request_method": ["GET", "HEAD"], "content_type": ["text/plain; charset=utf-8"], "cache_ttl": 300, "strategy": "memory"}`),
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, proxyCacheKongPlugin))
	t.Logf("deployed %s KongPlugin (%s) resource", client.ObjectKeyFromObject(proxyCacheKongPlugin), proxyCacheKongPlugin.PluginName)

	kongService := deployKongService(t, ctx, clientNamespaced,
		&configurationv1alpha1.KongService{
			Spec: configurationv1alpha1.KongServiceSpec{
				ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: cp.Name,
					},
				},
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					URL: lo.ToPtr("http://example.com"),
				},
			},
		},
	)

	pluginID := uuid.NewString()
	factory.SDK.PluginSDK.EXPECT().
		CreatePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
		Return(
			&sdkkonnectops.CreatePluginResponse{
				Plugin: &sdkkonnectcomp.Plugin{
					ID: lo.ToPtr(pluginID),
				},
			},
			nil,
		)
	factory.SDK.PluginSDK.EXPECT().
		UpsertPlugin(mock.Anything, mock.Anything, mock.Anything).Maybe().
		Return(
			&sdkkonnectops.UpsertPluginResponse{
				Plugin: &sdkkonnectcomp.Plugin{
					ID: lo.ToPtr(pluginID),
				},
			},
			nil,
		)

	wKongPlugin := setupWatch[configurationv1.KongPluginList](t, ctx, clientWithWatch, client.InNamespace(ns.Name))
	kpb := deployKongPluginBinding(t, ctx, clientNamespaced,
		&configurationv1alpha1.KongPluginBinding{
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: cp.Name,
					},
				},
				PluginReference: configurationv1alpha1.PluginRef{
					Name: proxyCacheKongPlugin.Name,
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
	t.Logf(
		"wait for the controller to pick the new unmanaged KongPluginBinding %s and put a %s finalizer on the referenced plugin %s",
		client.ObjectKeyFromObject(kpb),
		consts.PluginInUseFinalizer,
		client.ObjectKeyFromObject(proxyCacheKongPlugin),
	)
	_ = watchFor(t, ctx, wKongPlugin, watch.Modified,
		func(kp *configurationv1.KongPlugin) bool {
			return kp.Name == proxyCacheKongPlugin.Name &&
				controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
		},
		"KongPlugin wasn't updated to get the plugin-in-use finalizer",
	)
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.PluginSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	factory.SDK.PluginSDK.EXPECT().
		DeletePlugin(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
		Return(
			&sdkkonnectops.DeletePluginResponse{
				StatusCode: 200,
			},
			nil,
		)

	t.Logf("delete the KongPlugin %s, then check it does not get collected", client.ObjectKeyFromObject(proxyCacheKongPlugin))
	require.NoError(t, clientNamespaced.Delete(ctx, proxyCacheKongPlugin))
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.False(c, k8serrors.IsNotFound(
			clientNamespaced.Get(ctx, client.ObjectKeyFromObject(proxyCacheKongPlugin), proxyCacheKongPlugin),
		))
		assert.True(c, proxyCacheKongPlugin.DeletionTimestamp != nil)
		assert.True(c, controllerutil.ContainsFinalizer(proxyCacheKongPlugin, consts.PluginInUseFinalizer))
	}, waitTime, tickTime)

	t.Logf("delete the unmanaged KongPluginBinding %s, then check the proxy-cache KongPlugin %s gets collected",
		client.ObjectKeyFromObject(kpb),
		client.ObjectKeyFromObject(proxyCacheKongPlugin),
	)
	require.NoError(t, clientNamespaced.Delete(ctx, kpb))
	_ = watchFor(t, ctx, wKongPlugin, watch.Deleted,
		func(kp *configurationv1.KongPlugin) bool {
			return kp.Name == proxyCacheKongPlugin.Name
		},
		"KongPlugin did not got deleted but shouldn't have",
	)

	t.Logf(
		"delete the KongService %s and check it gets collected, as the KongPluginBinding finalizer should have been removed",
		client.ObjectKeyFromObject(kongService),
	)
	factory.SDK.ServicesSDK.EXPECT().
		DeleteService(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.Anything).
		Return(
			&sdkkonnectops.DeleteServiceResponse{
				StatusCode: 200,
			},
			nil,
		)
	require.NoError(t, clientNamespaced.Delete(ctx, kongService))
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, k8serrors.IsNotFound(
			clientNamespaced.Get(ctx, client.ObjectKeyFromObject(kongService), kongService),
		))
	}, waitTime, tickTime)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.ServicesSDK.AssertExpectations(t))
		assert.True(c, factory.SDK.PluginSDK.AssertExpectations(t))
	}, waitTime, tickTime)
}
