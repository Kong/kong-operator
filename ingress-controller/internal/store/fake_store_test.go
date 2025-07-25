package store

import (
	"errors"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	incubatorv1alpha1 "github.com/kong/kubernetes-configuration/api/incubator/v1alpha1"

	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	"github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
)

func TestNewFakeStoreEmpty(t *testing.T) {
	require.NotPanics(t, func() {
		s := NewFakeStoreEmpty()
		_, err := s.GetConfigMap("default", "foo")
		require.NotNil(t, s)
		require.ErrorAs(t, err, &NotFoundError{})
	})
}

func TestFakeStoreIngressV1(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	defaultClass := annotations.DefaultIngressClass
	ingresses := []*netv1.Ingress{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
			Spec: netv1.IngressSpec{
				Rules: []netv1.IngressRule{
					{
						Host: "example.com",
						IngressRuleValue: netv1.IngressRuleValue{
							HTTP: &netv1.HTTPIngressRuleValue{
								Paths: []netv1.HTTPIngressPath{
									{
										Path: "/",
										Backend: netv1.IngressBackend{
											Service: &netv1.IngressServiceBackend{
												Name: "foo-svc",
												Port: netv1.ServiceBackendPort{
													Name:   "http",
													Number: 80,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: "not-kong",
				},
			},
			Spec: netv1.IngressSpec{
				Rules: []netv1.IngressRule{
					{
						Host: "example.com",
						IngressRuleValue: netv1.IngressRuleValue{
							HTTP: &netv1.HTTPIngressRuleValue{
								Paths: []netv1.HTTPIngressPath{
									{
										Path: "/bar",
										Backend: netv1.IngressBackend{
											Service: &netv1.IngressServiceBackend{
												Name: "bar-svc",
												Port: netv1.ServiceBackendPort{
													Name:   "http",
													Number: 80,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: "skip-me-im-not-default",
				},
			},
			Spec: netv1.IngressSpec{
				Rules:            []netv1.IngressRule{},
				IngressClassName: &defaultClass,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "default",
			},
			Spec: netv1.IngressSpec{
				Rules:            []netv1.IngressRule{},
				IngressClassName: &defaultClass,
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{IngressesV1: ingresses})
	require.NoError(err)
	require.NotNil(store)
	assert.Len(store.ListIngressesV1(), 2)
}

func TestFakeStoreIngressClassV1(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	classes := []*netv1.IngressClass{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: netv1.IngressClassSpec{
				Controller: IngressClassKongController,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: netv1.IngressClassSpec{
				Controller: IngressClassKongController,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "baz",
			},
			Spec: netv1.IngressClassSpec{
				Controller: "some-other-controller.example.com/controller",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{IngressClassesV1: classes})
	require.NoError(err)
	require.NotNil(store)
	assert.Len(store.ListIngressClassesV1(), 2)
}

func TestFakeStoreListTCPIngress(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	ingresses := []*configurationv1beta1.TCPIngress{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
			Spec: configurationv1beta1.TCPIngressSpec{
				Rules: []configurationv1beta1.IngressRule{
					{
						Port: 9000,
						Backend: configurationv1beta1.IngressBackend{
							ServiceName: "foo-svc",
							ServicePort: 80,
						},
					},
				},
			},
		},
		{
			// this TCPIngress should *not* be loaded, as it lacks a class
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baz",
				Namespace: "default",
			},
			Spec: configurationv1beta1.TCPIngressSpec{
				Rules: []configurationv1beta1.IngressRule{
					{
						Port: 9000,
						Backend: configurationv1beta1.IngressBackend{
							ServiceName: "foo-svc",
							ServicePort: 80,
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: "not-kong",
				},
			},
			Spec: configurationv1beta1.TCPIngressSpec{
				Rules: []configurationv1beta1.IngressRule{
					{
						Port: 8000,
						Backend: configurationv1beta1.IngressBackend{
							ServiceName: "bar-svc",
							ServicePort: 80,
						},
					},
				},
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{TCPIngresses: ingresses})
	require.NoError(err)
	require.NotNil(store)
	ings, err := store.ListTCPIngresses()
	assert.NoError(err)
	assert.Len(ings, 1)
}

func TestFakeStoreService(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	services := []*corev1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{Services: services})
	require.NoError(err)
	require.NotNil(store)
	service, err := store.GetService("default", "foo")
	assert.NotNil(service)
	assert.NoError(err)

	service, err = store.GetService("default", "does-not-exists")
	assert.Error(err)
	assert.True(errors.As(err, &NotFoundError{}))
	assert.Nil(service)
}

func TestFakeStoreEndpointSlice(t *testing.T) {
	t.Parallel()
	endpoints := []*discoveryv1.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-1",
				Namespace: "default",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "foo",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-1",
				Namespace: "bar",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "foo",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-2",
				Namespace: "bar",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "foo",
				},
			},
		},
	}

	store, err := NewFakeStore(FakeObjects{EndpointSlices: endpoints})
	require.NoError(t, err)
	require.NotNil(t, store)

	t.Run("Get EndpointSlices for Service with single EndpointSlice", func(t *testing.T) {
		c, err := store.GetEndpointSlicesForService("default", "foo")
		require.NoError(t, err)
		require.Len(t, c, 1)
	})

	t.Run("Get EndpointSlices for Service with multiple EndpointSlices", func(t *testing.T) {
		c, err := store.GetEndpointSlicesForService("bar", "foo")
		require.NoError(t, err)
		require.Len(t, c, 2)
	})

	t.Run("Get EndpointSlices for non-existing Service", func(t *testing.T) {
		c, err := store.GetEndpointSlicesForService("default", "does-not-exist")
		require.ErrorAs(t, err, &NotFoundError{})
		require.Nil(t, c)
	})
}

func TestFakeStoreConsumer(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	consumers := []*configurationv1.KongConsumer{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{KongConsumers: consumers})
	require.NoError(err)
	require.NotNil(store)
	assert.Len(store.ListKongConsumers(), 1)
	c, err := store.GetKongConsumer("default", "foo")
	assert.NoError(err)
	assert.NotNil(c)

	c, err = store.GetKongConsumer("default", "does-not-exist")
	assert.Nil(c)
	assert.Error(err)
	assert.True(errors.As(err, &NotFoundError{}))
}

func TestFakeStoreConsumerGroup(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	consumerGroups := []*configurationv1beta1.KongConsumerGroup{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{KongConsumerGroups: consumerGroups})
	require.NoError(err)
	require.NotNil(store)
	assert.Len(store.ListKongConsumerGroups(), 1)
	c, err := store.GetKongConsumerGroup("default", "foo")
	assert.NoError(err)
	assert.NotNil(c)

	c, err = store.GetKongConsumerGroup("default", "does-not-exist")
	assert.Nil(c)
	assert.Error(err)
	assert.True(errors.As(err, &NotFoundError{}))
}

func TestFakeStorePlugins(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	plugins := []*configurationv1.KongPlugin{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{KongPlugins: plugins})
	assert.NoError(err)
	assert.NotNil(store)

	plugins = []*configurationv1.KongPlugin{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baz",
				Namespace: "default",
			},
		},
	}
	store, err = NewFakeStore(FakeObjects{KongPlugins: plugins})
	require.NoError(err)
	require.NotNil(store)
	plugins = store.ListKongPlugins()
	assert.Len(plugins, 1)

	plugin, err := store.GetKongPlugin("default", "does-not-exist")
	require.ErrorAs(err, &NotFoundError{})
	require.Nil(plugin)
}

func TestFakeStoreClusterPlugins(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	plugins := []*configurationv1.KongClusterPlugin{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{KongClusterPlugins: plugins})
	require.NoError(err)
	require.NotNil(store)
	plugins, err = store.ListGlobalKongClusterPlugins()
	assert.NoError(err)
	assert.Empty(plugins)

	plugins = []*configurationv1.KongClusterPlugin{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					"global": "true",
				},
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
		{
			// invalid due to lack of class, not loaded
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					"global": "true",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "baz",
			},
		},
	}
	store, err = NewFakeStore(FakeObjects{KongClusterPlugins: plugins})
	require.NoError(err)
	require.NotNil(store)
	plugins, err = store.ListGlobalKongClusterPlugins()
	assert.NoError(err)
	assert.Len(plugins, 1)

	plugin, err := store.GetKongClusterPlugin("foo")
	assert.NotNil(plugin)
	assert.NoError(err)

	plugin, err = store.GetKongClusterPlugin("does-not-exist")
	assert.Error(err)
	assert.True(errors.As(err, &NotFoundError{}))
	assert.Nil(plugin)
}

func TestFakeStoreSecret(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{Secrets: secrets})
	require.NoError(err)
	require.NotNil(store)
	secret, err := store.GetSecret("default", "foo")
	assert.NoError(err)
	assert.NotNil(secret)

	secret, err = store.GetSecret("default", "does-not-exist")
	assert.Nil(secret)
	assert.Error(err)
	assert.True(errors.As(err, &NotFoundError{}))
}

func TestFakeKongIngress(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	kongIngresses := []*configurationv1.KongIngress{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
		},
	}
	store, err := NewFakeStore(FakeObjects{KongIngresses: kongIngresses})
	require.NoError(err)
	require.NotNil(store)
	kingress, err := store.GetKongIngress("default", "foo")
	assert.NoError(err)
	assert.NotNil(kingress)

	kingress, err = store.GetKongIngress("default", "does-not-exist")
	assert.Error(err)
	assert.Nil(kingress)
	assert.True(errors.As(err, &NotFoundError{}))
}

func TestFakeStore_ListCACerts(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-secret",
				Namespace: "default",
			},
		},
	}
	configMaps := []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-configmap",
				Namespace: "default",
			},
		},
	}
	store, err := NewFakeStore(
		FakeObjects{
			Secrets:    secrets,
			ConfigMaps: configMaps,
		},
	)
	require.NoError(err)
	require.NotNil(store)
	secretCerts, configMapCerts, err := store.ListCACerts()
	assert.NoError(err)
	assert.Empty(secretCerts, "expect no secrets as CA certificates")
	assert.Empty(configMapCerts, "expect no configmaps as CA certificates")

	secrets = []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/ca-cert": "true",
				},
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo1",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/ca-cert": "true",
				},
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
	}
	store, err = NewFakeStore(FakeObjects{Secrets: secrets})
	require.NoError(err)
	require.NotNil(store)
	secretCerts, configMapCerts, err = store.ListCACerts()
	assert.NoError(err)
	assert.Len(secretCerts, 2, "expect two secrets as CA certificates")
	assert.Empty(configMapCerts, "expect 0 configmap as CA certificates")

	secrets = []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-secret",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/ca-cert": "true",
				},
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
	}
	configMaps = []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo-configmap",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/ca-cert": "true",
				},
				Annotations: map[string]string{
					annotations.IngressClassKey: annotations.DefaultIngressClass,
				},
			},
		},
	}
	store, err = NewFakeStore(
		FakeObjects{
			Secrets:    secrets,
			ConfigMaps: configMaps,
		},
	)
	require.NoError(err)
	require.NotNil(store)
	secretCerts, configMapCerts, err = store.ListCACerts()
	assert.NoError(err)
	assert.Len(secretCerts, 1, "expect 1 secret as CA certificates")
	assert.Len(configMapCerts, 1, "expect 1 configmap as CA certificates")
}

func TestFakeStoreHTTPRoute(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	classes := []*gatewayapi.HTTPRoute{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.HTTPRouteSpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.HTTPRouteSpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{HTTPRoutes: classes})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListHTTPRoutes()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two HTTPRoutes")
}

func TestFakeStoreUDPRoute(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	classes := []*gatewayapi.UDPRoute{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.UDPRouteSpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.UDPRouteSpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{UDPRoutes: classes})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListUDPRoutes()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two UDPRoutes")
}

func TestFakeStoreTCPRoute(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	classes := []*gatewayapi.TCPRoute{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.TCPRouteSpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.TCPRouteSpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{TCPRoutes: classes})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListTCPRoutes()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two TCPRoutes")
}

func TestFakeStoreTLSRoute(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	classes := []*gatewayapi.TLSRoute{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.TLSRouteSpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.TLSRouteSpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{TLSRoutes: classes})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListTLSRoutes()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two TLSRoutes")
}

func TestFakeStoreReferenceGrant(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	grants := []*gatewayapi.ReferenceGrant{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.ReferenceGrantSpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.ReferenceGrantSpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{ReferenceGrants: grants})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListReferenceGrants()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two ReferenceGrants")
}

func TestFakeStoreGateway(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	grants := []*gatewayapi.Gateway{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: gatewayapi.GatewaySpec{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
			Spec: gatewayapi.GatewaySpec{},
		},
	}
	store, err := NewFakeStore(FakeObjects{Gateways: grants})
	require.NoError(err)
	require.NotNil(store)
	routes, err := store.ListGateways()
	assert.NoError(err)
	assert.Len(routes, 2, "expect two Gateways")
}

func TestFakeStore_KongUpstreamPolicy(t *testing.T) {
	fakeObjects := FakeObjects{
		KongUpstreamPolicies: []*configurationv1beta1.KongUpstreamPolicy{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: configurationv1beta1.KongUpstreamPolicySpec{
					Algorithm: lo.ToPtr("least-connections"),
				},
			},
		},
	}
	store, err := NewFakeStore(fakeObjects)
	require.NoError(t, err)

	storedPolicy, err := store.GetKongUpstreamPolicy("default", "foo")
	require.NoError(t, err)
	require.Equal(t, fakeObjects.KongUpstreamPolicies[0], storedPolicy)
}

func TestFakeStore_KongServiceFacade(t *testing.T) {
	fakeObjects := FakeObjects{
		KongServiceFacades: []*incubatorv1alpha1.KongServiceFacade{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: incubatorv1alpha1.KongServiceFacadeSpec{
					Backend: incubatorv1alpha1.KongServiceFacadeBackend{
						Name: "service-name",
						Port: 80,
					},
				},
			},
		},
	}

	store, err := NewFakeStore(fakeObjects)
	require.NoError(t, err)

	storedFacade, err := store.GetKongServiceFacade("default", "foo")
	require.NoError(t, err)
	require.Equal(t, fakeObjects.KongServiceFacades[0], storedFacade)
}
