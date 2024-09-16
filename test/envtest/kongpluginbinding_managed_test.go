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

func TestKongPluginBindingManaged(t *testing.T) {
	const (
		konnectSyncTime = 100 * time.Millisecond
		waitTime        = 30 * time.Second
		tickTime        = 100 * time.Millisecond
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

	apiAuth := deployKonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
	cp := deployKonnectGatewayControlPlane(t, ctx, clientNamespaced, apiAuth)

	factory := ops.NewMockSDKFactory(t)
	serviceID := uuid.NewString()
	factory.SDK.ServicesSDK.EXPECT().
		CreateService(mock.Anything, cp.Status.ID, mock.Anything).
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

	pluginID := uuid.NewString()
	factory.SDK.PluginSDK.EXPECT().
		CreatePlugin(mock.Anything, mock.Anything, mock.Anything).
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

	kongService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kongservice-1",
			Annotations: map[string]string{
				consts.PluginsAnnotationKey: rateLimitingkongPlugin.Name,
			},
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr("kongservice-1"),
				URL:  lo.ToPtr("http://example.com"),
			},
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, kongService))
	t.Logf("deployed %s KongService resource", client.ObjectKeyFromObject(kongService))

	t.Logf("waiting for KongPluginBinding to be created")
	w, err := clientWithWatch.Watch(ctx, &configurationv1alpha1.KongPluginBindingList{}, client.InNamespace(ns.Name))
	require.NoError(t, err)
	t.Cleanup(func() { w.Stop() })
	kongPluginBinding := watchFor(t, ctx, w, watch.Added,
		func(kpb *configurationv1alpha1.KongPluginBinding) bool {
			return kpb.Spec.Targets.ServiceReference != nil &&
				kpb.Spec.Targets.ServiceReference.Name == kongService.Name &&
				kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
		},
		"KongPluginBinding wasn't created",
	)

	t.Logf("delete managed kongPluginBinding %s, then check it gets recreated", client.ObjectKeyFromObject(kongPluginBinding))
	require.NoError(t, clientNamespaced.Delete(ctx, kongPluginBinding))
	kongPluginBinding = watchFor(t, ctx, w, watch.Added,
		func(kpb *configurationv1alpha1.KongPluginBinding) bool {
			return kpb.Spec.Targets.ServiceReference != nil &&
				kpb.Spec.Targets.ServiceReference.Name == kongService.Name &&
				kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
		},
		"KongPluginBinding wasn't recreated",
	)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.ServicesSDK.AssertExpectations(t))
		assert.True(c, factory.SDK.PluginSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Logf(
		"remove annotation from KongService %s and check that managed KongPluginBinding %s gets deleted, "+
			"then check that gateway.konghq.com/plugin-in-use finalizer gets removed from %s KongPlugin",
		client.ObjectKeyFromObject(kongService),
		client.ObjectKeyFromObject(kongPluginBinding),
		client.ObjectKeyFromObject(rateLimitingkongPlugin),
	)

	factory.SDK.PluginSDK.EXPECT().
		DeletePlugin(mock.Anything, cp.GetKonnectStatus().ID, mock.Anything).
		Return(
			&sdkkonnectops.DeletePluginResponse{
				StatusCode: 200,
			},
			nil,
		)

	wPlugins, err := clientWithWatch.Watch(ctx, &configurationv1.KongPluginList{}, client.InNamespace(ns.Name))
	require.NoError(t, err)
	t.Cleanup(func() { wPlugins.Stop() })

	wPluginBindings, err := clientWithWatch.Watch(ctx, &configurationv1alpha1.KongPluginBindingList{}, client.InNamespace(ns.Name))
	require.NoError(t, err)
	t.Cleanup(func() { wPluginBindings.Stop() })

	kongServiceToPatch := kongService.DeepCopy()
	delete(kongServiceToPatch.Annotations, consts.PluginsAnnotationKey)
	require.NoError(t, clientNamespaced.Patch(ctx, kongServiceToPatch, client.MergeFrom(kongService)))

	t.Logf(
		"checking that managed KongPluginBinding %s gets deleted",
		client.ObjectKeyFromObject(kongPluginBinding),
	)
	_ = watchFor(t, ctx, wPluginBindings, watch.Deleted,
		func(kpb *configurationv1alpha1.KongPluginBinding) bool {
			return kpb.Spec.Targets.ServiceReference != nil &&
				kpb.Spec.Targets.ServiceReference.Name == kongService.Name &&
				kpb.Spec.PluginReference.Name == rateLimitingkongPlugin.Name
		},
		"KongPluginBinding wasn't deleted after removing annotation from KongService",
	)
	t.Logf(
		"checking that managed KongPlugin %s gets plugin-in-use finalizer removed",
		client.ObjectKeyFromObject(rateLimitingkongPlugin),
	)
	_ = watchFor(t, ctx, wPlugins, watch.Modified,
		func(kp *configurationv1.KongPlugin) bool {
			return kp.Name == rateLimitingkongPlugin.Name &&
				!controllerutil.ContainsFinalizer(kp, consts.PluginInUseFinalizer)
		},
		"KongPlugin wasn't updated to get plugin-in-use finalizer removed",
	)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.PluginSDK.AssertExpectations(t))
	}, waitTime, tickTime)
}
