package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func TestMapHTTPRouteForClientCertSecret(t *testing.T) {
	ctx := context.Background()

	scheme := schemeWithAll()
	require.NoError(t, gatewayv1.Install(scheme))

	secret := &corev1.Secret{
		Name: "my-cert", Namespace: "ns1",
	}
	svcWithAnnotation := &corev1.Service{
		Name:      "svc1",
		Namespace: "ns1",
		Annotations: map[string]string{
			"konghq.com/client-cert": "my-cert",
		},
	}
	svcOtherAnnotation := &corev1.Service{
		Name:      "svc2",
		Namespace: "ns1",
		Annotations: map[string]string{
			"konghq.com/client-cert": "other-cert",
		},
	}
	route1 := &gwtypes.HTTPRoute{
		Name: "route1", Namespace: "ns1",
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					Name: "svc1",
				}},
			}},
		},
	}

	httpRouteIndexer := func(obj client.Object) []string {
		route, ok := obj.(*gwtypes.HTTPRoute)
		if !ok {
			return nil
		}
		var keys []string
		for _, rule := range route.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				keys = append(keys, route.Namespace+"/"+string(ref.Name))
			}
		}
		return keys
	}

	tests := []struct {
		name       string
		input      client.Object
		objects    []client.Object
		setupIndex bool
		wantLen    int
		wantNames  []string
		wantNil    bool
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name:       "secret not referenced by any service returns empty",
			input:      secret,
			setupIndex: true,
			objects:    []client.Object{},
			wantLen:    0,
		},
		{
			name:       "secret referenced by service with HTTPRoute returns request",
			input:      secret,
			objects:    []client.Object{svcWithAnnotation, route1},
			setupIndex: true,
			wantLen:    1,
			wantNames:  []string{"route1"},
		},
		{
			name:       "service annotation references different secret - no match",
			input:      secret,
			objects:    []client.Object{svcOtherAnnotation},
			setupIndex: true,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.setupIndex {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.objects...).
					WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, httpRouteIndexer).
					Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			mapFn := MapHTTPRouteForClientCertSecret(cl)
			result := mapFn(ctx, tt.input)

			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.Len(t, result, tt.wantLen)
			if len(tt.wantNames) > 0 {
				names := make([]string, len(result))
				for i, r := range result {
					names[i] = r.Name
				}
				for _, want := range tt.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}

func TestMapGRPCRouteForClientCertSecret(t *testing.T) {
	ctx := context.Background()

	scheme := schemeWithAll()
	require.NoError(t, gatewayv1.Install(scheme))

	secret := &corev1.Secret{
		Name: "my-cert", Namespace: "ns1",
	}
	svcWithAnnotation := &corev1.Service{
		Name:      "svc1",
		Namespace: "ns1",
		Annotations: map[string]string{
			"konghq.com/client-cert": "my-cert",
		},
	}
	svcOtherAnnotation := &corev1.Service{
		Name:      "svc2",
		Namespace: "ns1",
		Annotations: map[string]string{
			"konghq.com/client-cert": "other-cert",
		},
	}
	route1 := &gwtypes.GRPCRoute{
		Name: "route1", Namespace: "ns1",
		Spec: gwtypes.GRPCRouteSpec{
			Rules: []gwtypes.GRPCRouteRule{{
				BackendRefs: []gwtypes.GRPCBackendRef{{
					Name: "svc1",
				}},
			}},
		},
	}

	grpcRouteIndexer := func(obj client.Object) []string {
		route, ok := obj.(*gwtypes.GRPCRoute)
		if !ok {
			return nil
		}
		var keys []string
		for _, rule := range route.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				keys = append(keys, route.Namespace+"/"+string(ref.Name))
			}
		}
		return keys
	}

	tests := []struct {
		name       string
		input      client.Object
		objects    []client.Object
		setupIndex bool
		wantLen    int
		wantNames  []string
		wantNil    bool
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name:       "secret not referenced by any service returns empty",
			input:      secret,
			setupIndex: true,
			objects:    []client.Object{},
			wantLen:    0,
		},
		{
			name:       "secret referenced by service with GRPCRoute returns request",
			input:      secret,
			objects:    []client.Object{svcWithAnnotation, route1},
			setupIndex: true,
			wantLen:    1,
			wantNames:  []string{"route1"},
		},
		{
			name:       "service annotation references different secret - no match",
			input:      secret,
			objects:    []client.Object{svcOtherAnnotation},
			setupIndex: true,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.setupIndex {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.objects...).
					WithIndex(&gwtypes.GRPCRoute{}, index.BackendServicesOnGRPCRouteIndex, grpcRouteIndexer).
					Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			mapFn := MapGRPCRouteForClientCertSecret(cl)
			result := mapFn(ctx, tt.input)

			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.Len(t, result, tt.wantLen)
			if len(tt.wantNames) > 0 {
				names := make([]string, len(result))
				for i, r := range result {
					names[i] = r.Name
				}
				for _, want := range tt.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}

func TestMapTLSRouteForClientCertSecret(t *testing.T) {
	ctx := context.Background()

	scheme := schemeWithAll()
	require.NoError(t, gatewayv1.Install(scheme))

	secret := &corev1.Secret{
		Name: "my-cert", Namespace: "ns1",
	}
	svcWithAnnotation := &corev1.Service{
		Name:      "svc1",
		Namespace: "ns1",
		Annotations: map[string]string{
			"konghq.com/client-cert": "my-cert",
		},
	}
	tlsRoute1 := &gwtypes.TLSRoute{
		Name: "tlsroute1", Namespace: "ns1",
		Spec: gwtypes.TLSRouteSpec{
			Rules: []gwtypes.TLSRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					Name: "svc1",
				}},
			}},
		},
	}

	tlsRouteIndexer := func(obj client.Object) []string {
		route, ok := obj.(*gwtypes.TLSRoute)
		if !ok {
			return nil
		}
		var keys []string
		for _, rule := range route.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				keys = append(keys, route.Namespace+"/"+string(ref.Name))
			}
		}
		return keys
	}

	tests := []struct {
		name       string
		input      client.Object
		objects    []client.Object
		setupIndex bool
		wantLen    int
		wantNames  []string
		wantNil    bool
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name:       "secret not referenced returns empty",
			input:      secret,
			objects:    []client.Object{},
			setupIndex: true,
			wantLen:    0,
		},
		{
			name:       "secret referenced by service with TLSRoute returns request",
			input:      secret,
			objects:    []client.Object{svcWithAnnotation, tlsRoute1},
			setupIndex: true,
			wantLen:    1,
			wantNames:  []string{"tlsroute1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.setupIndex {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.objects...).
					WithIndex(&gwtypes.TLSRoute{}, index.BackendServicesOnTLSRouteIndex, tlsRouteIndexer).
					Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			mapFn := MapTLSRouteForClientCertSecret(cl)
			result := mapFn(ctx, tt.input)

			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.Len(t, result, tt.wantLen)
			if len(tt.wantNames) > 0 {
				names := make([]string, len(result))
				for i, r := range result {
					names[i] = r.Name
				}
				for _, want := range tt.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}

// pluginConfigSecretPlugin returns a KongPlugin sourcing its config from the given secret names:
// the first through spec.configFrom, the remaining ones through spec.configPatches.
func pluginConfigSecretPlugin(name, namespace string, secretNames ...string) *configurationv1.KongPlugin {
	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		PluginName: "openid-connect",
	}
	for i, secretName := range secretNames {
		if i == 0 {
			plugin.ConfigFrom = &configurationv1.ConfigSource{
				SecretValue: configurationv1.SecretValueFromSource{Secret: secretName, Key: "config"},
			}
			continue
		}
		plugin.ConfigPatches = append(plugin.ConfigPatches, configurationv1.ConfigPatch{
			Path: "/client_secret",
			ValueFrom: configurationv1.ConfigSource{
				SecretValue: configurationv1.SecretValueFromSource{Secret: secretName, Key: "client_secret"},
			},
		})
	}
	return plugin
}

// extensionRefHTTPRoute returns an HTTPRoute in namespace "ns1" referencing the KongPlugin
// "ns1/oidc" in an ExtensionRef filter.
func extensionRefHTTPRoute(name string) *gwtypes.HTTPRoute {
	return &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns1"},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				Filters: []gwtypes.HTTPRouteFilter{{
					Type: gatewayv1.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gatewayv1.LocalObjectReference{
						Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
						Kind:  "KongPlugin",
						Name:  "oidc",
					},
				}},
			}},
		},
	}
}

// extensionRefGRPCRoute returns a GRPCRoute in namespace "ns1" referencing the KongPlugin
// "ns1/oidc" in an ExtensionRef filter.
func extensionRefGRPCRoute(name string) *gwtypes.GRPCRoute {
	return &gwtypes.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns1"},
		Spec: gwtypes.GRPCRouteSpec{
			Rules: []gwtypes.GRPCRouteRule{{
				Filters: []gatewayv1.GRPCRouteFilter{{
					Type: gatewayv1.GRPCRouteFilterExtensionRef,
					ExtensionRef: &gatewayv1.LocalObjectReference{
						Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
						Kind:  "KongPlugin",
						Name:  "oidc",
					},
				}},
			}},
		},
	}
}

func TestMapHTTPRouteForPluginConfigSecret(t *testing.T) {
	ctx := context.Background()
	s := schemeWithAll()

	testCases := []struct {
		name      string
		input     client.Object
		objects   []client.Object
		wantNames []string
	}{
		{
			name:      "non Secret input is ignored",
			input:     &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns1"}},
			wantNames: nil,
		},
		{
			name:  "secret referenced through configFrom",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefHTTPRoute("route1"),
			},
			wantNames: []string{"route1"},
		},
		{
			name:  "secret referenced through configPatches",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "patch", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg", "patch"),
				extensionRefHTTPRoute("route1"),
			},
			wantNames: []string{"route1"},
		},
		{
			name:  "secret not referenced by any plugin",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefHTTPRoute("route1"),
			},
			wantNames: nil,
		},
		{
			name:  "plugin in another namespace is not matched",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns2"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefHTTPRoute("route1"),
			},
			wantNames: nil,
		},
		{
			name:  "several routes referencing the same plugin",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefHTTPRoute("route1"),
				extensionRefHTTPRoute("route2"),
			},
			wantNames: []string{"route1", "route2"},
		},
		{
			name:  "a route referencing two plugins backed by the same secret is enqueued once",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				pluginConfigSecretPlugin("other", "ns1", "cfg"),
				func() client.Object {
					route := extensionRefHTTPRoute("route1")
					route.Spec.Rules[0].Filters = append(route.Spec.Rules[0].Filters, gwtypes.HTTPRouteFilter{
						Type: gatewayv1.HTTPRouteFilterExtensionRef,
						ExtensionRef: &gatewayv1.LocalObjectReference{
							Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
							Kind:  "KongPlugin",
							Name:  "other",
						},
					})
					return route
				}(),
			},
			wantNames: []string{"route1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.objects...).
				WithIndex(&gwtypes.HTTPRoute{}, index.KongPluginsOnHTTPRouteIndex, index.KongPluginsOnHTTPRoute).
				Build()

			requests := MapHTTPRouteForPluginConfigSecret(cl)(ctx, tc.input)
			names := make([]string, 0, len(requests))
			for _, req := range requests {
				names = append(names, req.Name)
			}
			require.ElementsMatch(t, tc.wantNames, names)
			require.Len(t, names, len(tc.wantNames))
		})
	}
}

func TestMapGRPCRouteForPluginConfigSecret(t *testing.T) {
	ctx := context.Background()
	s := schemeWithAll()

	testCases := []struct {
		name      string
		input     client.Object
		objects   []client.Object
		wantNames []string
	}{
		{
			name:      "non Secret input is ignored",
			input:     &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns1"}},
			wantNames: nil,
		},
		{
			name:  "secret referenced through configFrom",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefGRPCRoute("route1"),
			},
			wantNames: []string{"route1"},
		},
		{
			name:  "secret referenced through configPatches",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "patch", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg", "patch"),
				extensionRefGRPCRoute("route1"),
			},
			wantNames: []string{"route1"},
		},
		{
			name:  "secret not referenced by any plugin",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefGRPCRoute("route1"),
			},
			wantNames: nil,
		},
		{
			name:  "several routes referencing the same plugin",
			input: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cfg", Namespace: "ns1"}},
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				extensionRefGRPCRoute("route1"),
				extensionRefGRPCRoute("route2"),
			},
			wantNames: []string{"route1", "route2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.objects...).
				WithIndex(&gwtypes.GRPCRoute{}, index.KongPluginsOnGRPCRouteIndex, index.KongPluginsOnGRPCRoute).
				Build()

			requests := MapGRPCRouteForPluginConfigSecret(cl)(ctx, tc.input)
			names := make([]string, 0, len(requests))
			for _, req := range requests {
				names = append(names, req.Name)
			}
			require.ElementsMatch(t, tc.wantNames, names)
			require.Len(t, names, len(tc.wantNames))
		})
	}
}

func TestKongPluginsForConfigSecret(t *testing.T) {
	ctx := context.Background()
	s := schemeWithAll()

	testCases := []struct {
		name       string
		namespace  string
		secretName string
		objects    []client.Object
		want       []string
	}{
		{
			name:       "no plugins at all",
			namespace:  "ns1",
			secretName: "cfg",
			want:       nil,
		},
		{
			name:       "plugin without secret references",
			namespace:  "ns1",
			secretName: "cfg",
			objects:    []client.Object{pluginConfigSecretPlugin("plain", "ns1")},
			want:       nil,
		},
		{
			name:       "matching plugin",
			namespace:  "ns1",
			secretName: "cfg",
			objects: []client.Object{
				pluginConfigSecretPlugin("oidc", "ns1", "cfg"),
				pluginConfigSecretPlugin("other", "ns1", "unrelated"),
			},
			want: []string{"ns1/oidc"},
		},
		{
			name:       "several matching plugins",
			namespace:  "ns1",
			secretName: "cfg",
			objects: []client.Object{
				pluginConfigSecretPlugin("first", "ns1", "cfg"),
				pluginConfigSecretPlugin("second", "ns1", "other", "cfg"),
			},
			want: []string{"ns1/first", "ns1/second"},
		},
		{
			name:       "plugins in other namespaces are ignored",
			namespace:  "ns1",
			secretName: "cfg",
			objects:    []client.Object{pluginConfigSecretPlugin("oidc", "ns2", "cfg")},
			want:       nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(s).WithObjects(tc.objects...).Build()
			require.ElementsMatch(t, tc.want, kongPluginsForConfigSecret(ctx, cl, tc.namespace, tc.secretName))
		})
	}
}
