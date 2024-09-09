package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestKongPlugins(t *testing.T) {
	t.Parallel()
	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]

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
	err := GetClients().MgrClient.Create(GetCtx(), rateLimitingkongPlugin)
	require.NoError(t, err)
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
	err = GetClients().MgrClient.Create(GetCtx(), proxyCachekongPlugin)
	require.NoError(t, err)
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
	err = GetClients().MgrClient.Create(GetCtx(), kongService)
	require.NoError(t, err)
	cleaner.Add(kongService)

	t.Log("waiting for the managed KongPluginBinding to be created")
	managedKPB := &configurationv1alpha1.KongPluginBinding{}
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := GetClients().MgrClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) != 1 {
			return false
		}
		managedKPB = &kongPluginBindingList.Items[0]
		return true
	}, 30*time.Second, time.Second)

	t.Log("delete managed kongPluginBinding, then check it gets recreated")
	err = GetClients().MgrClient.Delete(GetCtx(), managedKPB)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := GetClients().MgrClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) != 1 {
			return false
		}
		managedKPB = &kongPluginBindingList.Items[0]
		return true
	}, 30*time.Second, time.Second)

	t.Log("remove annotation from kongservice")
	newKongService := kongService.DeepCopy()
	newKongService.Annotations = nil
	require.Eventually(t, func() bool {
		if err := GetClients().MgrClient.Patch(GetCtx(), newKongService, client.MergeFrom(kongService)); err != nil {
			return false
		}
		return true
	}, 30*time.Second, 5*time.Second)

	t.Log("then check the managed kpb gets deleted")
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := GetClients().MgrClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		if len(kongPluginBindingList.Items) > 0 {
			return false
		}
		return true
	}, 15*time.Second, time.Second)

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
	err = GetClients().MgrClient.Create(GetCtx(), unmanagedKPB)
	require.NoError(t, err)
	cleaner.Add(unmanagedKPB)

	t.Log("wait for the controller to pick the new unmanaged kongPluginBinding and put a finalizer on the referenced plugin")
	require.Eventually(t, func() bool {
		if err = GetClients().MgrClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin); err != nil {
			return false
		}
		return controllerutil.ContainsFinalizer(proxyCachekongPlugin, consts.PluginInUseFinalizer)
	}, 15*time.Second, time.Second)

	t.Log("delete the proxy-cache plugin, then check it does not get collected")
	err = GetClients().MgrClient.Delete(GetCtx(), proxyCachekongPlugin)
	require.NoError(t, err)
	require.Never(t, func() bool {
		err := GetClients().MgrClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin)
		return k8serrors.IsNotFound(err)
	}, 15*time.Second, time.Second)

	t.Log("delete the unmanaged kongPluginBinding, then check the proxy-cache kongPlugin gets collected")
	err = GetClients().MgrClient.Delete(GetCtx(), unmanagedKPB)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		kongPluginBindingList := &configurationv1alpha1.KongPluginBindingList{}
		err := GetClients().MgrClient.List(GetCtx(), kongPluginBindingList, client.InNamespace(ns.Name))
		if err != nil {
			return false
		}
		return len(kongPluginBindingList.Items) == 0
	}, 15*time.Second, time.Second)
	require.Eventually(t, func() bool {
		err := GetClients().MgrClient.Get(GetCtx(), client.ObjectKeyFromObject(proxyCachekongPlugin), proxyCachekongPlugin)
		return k8serrors.IsNotFound(err)
	}, 15*time.Second, time.Second)

	t.Log("delete the kongservice and check it gets collected, as the kongPluginBinding finalizer should have been removed")
	err = GetClients().MgrClient.Delete(GetCtx(), kongService)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		err := GetClients().MgrClient.Get(GetCtx(), client.ObjectKeyFromObject(kongService), kongService)
		return k8serrors.IsNotFound(err)
	}, 15*time.Second, time.Second)
}
