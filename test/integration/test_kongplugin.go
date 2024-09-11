package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/helpers"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// TestKongPlugins is an integration that aims at testing the KongPluginBinding resources
// are properly created and cleaned up when Konnect entities get annotated with KongPlugin names.
// It also tests that unmanaged KongPluginBindings are properly handled when managed KongPluginBindings
// are in play.
// NOTE: this test does not test the Konnect integration. No resource created here will be pushed to Konnect.
func TestKongPlugins(t *testing.T) {
	t.Parallel()
	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.

	const (
		tickTime = 500 * time.Millisecond
		timeout  = 30 * time.Second
	)
	var (
		testID        = uuid.NewString()[:8]
		managerClient = GetClients().MgrClient
	)

	ns, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	rateLimitingkongPluginName := "rate-limiting-kp-" + testID
	t.Logf("deploying %s KongPlugin resource", rateLimitingkongPluginName)
	rateLimitingkongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rateLimitingkongPluginName,
			Namespace: ns.Name,
		},
		PluginName: "rate-limiting",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"minute": 5, "policy": "local"}`),
		},
	}
	require.NoError(t, managerClient.Create(GetCtx(), rateLimitingkongPlugin))
	cleaner.Add(rateLimitingkongPlugin)

	proxyCachekongPluginName := "proxy-cache-kp-" + testID
	t.Logf("deploying %s KongPlugin resource", proxyCachekongPluginName)
	proxyCachekongPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      proxyCachekongPluginName,
			Namespace: ns.Name,
		},
		PluginName: "proxy-cache",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"response_code": [200], "request_method": ["GET", "HEAD"], "content_type": ["text/plain; charset=utf-8"], "cache_ttl": 300, "strategy": "memory"}`),
		},
	}
	require.NoError(t, managerClient.Create(GetCtx(), proxyCachekongPlugin))
	cleaner.Add(proxyCachekongPlugin)

	ksName := "ks-" + testID
	t.Logf("deploying KongService %s resource", ksName)
	kongService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ksName,
			Namespace: ns.Name,
			Annotations: map[string]string{
				consts.PluginsAnnotationKey: rateLimitingkongPluginName,
			},
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr(ksName),
				URL:  lo.ToPtr("http://example.com"),
			},
		},
	}
	require.NoError(t, managerClient.Create(GetCtx(), kongService))
	cleaner.Add(kongService)

	t.Logf("waiting for the managed KongPluginBinding to be created")
	managedKPB := &configurationv1alpha1.KongPluginBinding{}
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := managerClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) != 1 {
			return false
		}
		managedKPB = &kongPluginBindingList.Items[0]
		return true
	}, timeout, tickTime)

	t.Logf("delete managed kongPluginBinding %s, then check it gets recreated", managedKPB.Name)
	require.NoError(t, managerClient.Delete(GetCtx(), managedKPB))
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := managerClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) != 1 {
			return false
		}
		managedKPB = &kongPluginBindingList.Items[0]
		return true
	}, timeout, tickTime)

	t.Logf("remove annotation from kongservice %s", kongService.Name)
	newKongService := kongService.DeepCopy()
	newKongService.Annotations = nil
	require.Eventually(t, func() bool {
		if err := managerClient.Patch(GetCtx(), newKongService, client.MergeFrom(kongService)); err != nil {
			return false
		}
		return true
	}, timeout, tickTime)

	t.Log("then check the managed KongPluginBinding gets deleted")
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := managerClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) > 0 {
			return false
		}
		return true
	}, timeout, tickTime)

	unmanagedKPBName := "kpb-" + testID
	t.Logf("deploying unmanaged KongPluginBinding %s resource", unmanagedKPBName)
	unmanagedKPB := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      unmanagedKPBName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: proxyCachekongPluginName,
			},
			Targets: configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Group: configurationv1alpha1.GroupVersion.Group,
					Kind:  "KongService",
					Name:  kongService.Name,
				},
			},
		},
	}
	require.NoError(t, managerClient.Create(GetCtx(), unmanagedKPB))
	cleaner.Add(unmanagedKPB)

	t.Log("wait for the controller to pick the new unmanaged kongPluginBinding and put a finalizer on the referenced plugin")
	require.Eventually(t, func() bool {
		if err := managerClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin); err != nil {
			return false
		}
		return controllerutil.ContainsFinalizer(proxyCachekongPlugin, consts.PluginInUseFinalizer)
	}, timeout, tickTime)

	t.Logf("delete the plugin %s, then check it does not get collected", proxyCachekongPluginName)
	require.NoError(t, managerClient.Delete(GetCtx(), proxyCachekongPlugin))
	require.Never(t, func() bool {
		err := managerClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin)
		return k8serrors.IsNotFound(err)
	}, timeout, tickTime)

	t.Logf("delete the unmanaged kongPluginBinding %s, then check the proxy-cache kongPlugin gets collected", unmanagedKPBName)
	require.NoError(t, managerClient.Delete(GetCtx(), unmanagedKPB))
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := managerClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		return len(kongPluginBindingList.Items) == 0
	}, timeout, tickTime)
	require.Eventually(t, func() bool {
		return k8serrors.IsNotFound(managerClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin))
	}, timeout, tickTime)

	t.Logf("delete the kongservice %s and check it gets collected, as the kongPluginBinding finalizer should have been removed", ksName)
	require.NoError(t, managerClient.Delete(GetCtx(), kongService))
	require.Eventually(t, func() bool {
		return k8serrors.IsNotFound(managerClient.Get(GetCtx(), client.ObjectKeyFromObject(kongService), kongService))
	}, timeout, tickTime)
}
