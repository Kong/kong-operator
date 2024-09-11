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

	t.Log("deploying a rate-limiting KongPlugin resource")
	rateLimitingkongPluginName := "rate-limiting-kp-" + testID
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

	t.Log("deploying a proxy-cache KongPlugin resource")
	proxyCachekongPluginName := "proxy-cache-kp-" + testID
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

	t.Log("deploying a KongService resource")
	ksName := "ks-" + testID
	kongService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ksName,
			Namespace: ns.Name,
			Annotations: map[string]string{
				"konghq.com/plugins": rateLimitingkongPluginName,
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

	t.Log("waiting for the managed KongPluginBinding to be created")
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

	t.Log("delete managed kongPluginBinding, then check it gets recreated")
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

	t.Log("remove annotation from kongservice")
	newKongService := kongService.DeepCopy()
	newKongService.Annotations = nil
	require.Eventually(t, func() bool {
		if err := managerClient.Patch(GetCtx(), newKongService, client.MergeFrom(kongService)); err != nil {
			return false
		}
		return true
	}, timeout, tickTime)

	t.Log("then check the managed kpb gets deleted")
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

	t.Log("deploying an unmanaged KongPluginBinding resource")
	unmanagedKPBName := "kpb-" + testID
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

	t.Log("delete the proxy-cache plugin, then check it does not get collected")
	require.NoError(t, managerClient.Delete(GetCtx(), proxyCachekongPlugin))
	require.Never(t, func() bool {
		err := managerClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin)
		return k8serrors.IsNotFound(err)
	}, timeout, tickTime)

	t.Log("delete the unmanaged kongPluginBinding, then check the proxy-cache kongPlugin gets collected")
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

	t.Log("delete the kongservice and check it gets collected, as the kongPluginBinding finalizer should have been removed")
	require.NoError(t, managerClient.Delete(GetCtx(), kongService))
	require.Eventually(t, func() bool {
		return k8serrors.IsNotFound(managerClient.Get(GetCtx(), client.ObjectKeyFromObject(kongService), kongService))
	}, timeout, tickTime)
}
