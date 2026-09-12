package plugin

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
)

var (
	httpRouteTypeMeta = metav1.TypeMeta{
		Kind:       "HTTPRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
	}
)

func TestAppendHTTPRouteToPluginAnnotations(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name                string
		existingAnnotations map[string]string
		httpRoute           *gwtypes.HTTPRoute
		expectedAnnotation  string
		expectModification  bool
	}{
		{
			name:                "no existing annotations",
			existingAnnotations: nil,
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "empty hybrid-routes annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "existing different route in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			expectedAnnotation: "other-namespace/other-route,test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "route already exists in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: false,
		},
		{
			name: "multiple existing routes, adding new one",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "route3",
				Namespace: "ns3",
			},
			expectedAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &configurationv1.KongPlugin{
				Name:        "test-plugin",
				Namespace:   "test-namespace",
				Annotations: tt.existingAnnotations,
			}

			am := metadata.NewAnnotationManager(logger)
			am.AppendRouteToAnnotation(plugin, tt.httpRoute)
			actualAnnotation := plugin.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestPluginForFilter(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	tests := []struct {
		name           string
		filter         gwtypes.HTTPRouteFilter
		rule           gwtypes.HTTPRouteRule
		existingPlugin *configurationv1.KongPlugin
		httpRoute      *gwtypes.HTTPRoute
		parentRef      *gwtypes.ParentReference
		expectedError  bool
		validatePlugin func(t *testing.T, plugin *configurationv1.KongPlugin)
	}{
		{
			name: "create new request header modifier plugin",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/test"),
						},
					},
				},
			},
			existingPlugin: nil,
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			expectedError: false,
			validatePlugin: func(t *testing.T, plugin *configurationv1.KongPlugin) {
				require.NotNil(t, plugin)
				assert.Equal(t, "test-namespace", plugin.Namespace)
				assert.Equal(t, "request-transformer", plugin.PluginName)
				assert.Contains(t, plugin.Annotations, consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation)
				assert.Equal(t, "test-namespace/test-route", plugin.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
			},
		},
		{
			name: "route tags are not merged into the tags annotation",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
					},
				},
			},
			rule: gwtypes.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Type:  new(gatewayv1.PathMatchPathPrefix),
							Value: new("/test"),
						},
					},
				},
			},
			existingPlugin: nil,
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-route",
				Namespace: "test-namespace",
				UID:       "test-uid",
				Annotations: map[string]string{
					pkgmetadata.AnnotationKeyTags: "team-b, team-a",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			expectedError: false,
			validatePlugin: func(t *testing.T, plugin *configurationv1.KongPlugin) {
				require.NotNil(t, plugin)
				assert.Empty(t, plugin.Annotations[pkgmetadata.AnnotationKeyTags])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := scheme.Get()
			var objects []runtime.Object
			if tt.existingPlugin != nil {
				objects = append(objects, tt.existingPlugin)
			}
			fakeClient := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			rule := tt.rule
			rule.Filters = append(rule.Filters, tt.filter)
			plugins, err := PluginsForRule(ctx, logger, fakeClient, tt.httpRoute, rule, tt.parentRef)

			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.validatePlugin != nil {
				for _, p := range plugins {
					tt.validatePlugin(t, &p)
				}
			}
		})
	}
}

func TestGetReferencedKongPlugin(t *testing.T) {
	tests := []struct {
		name           string
		filter         gwtypes.HTTPRouteFilter
		namespace      string
		existingPlugin *configurationv1.KongPlugin
		expectedPlugin *configurationv1.KongPlugin
		expectedError  string
	}{
		{
			name: "nil ExtensionRef",
			filter: gwtypes.HTTPRouteFilter{
				Type:         gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: nil,
			},
			namespace:     "default",
			expectedError: "ExtensionRef filter is missing",
		},
		{
			name: "unsupported ExtensionRef group",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group("unsupported.group"),
					Kind:  "KongPlugin",
					Name:  "test-plugin",
				},
			},
			namespace:     "default",
			expectedError: "unsupported ExtensionRef: unsupported.group/KongPlugin",
		},
		{
			name: "unsupported ExtensionRef kind",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "UnsupportedKind",
					Name:  "test-plugin",
				},
			},
			namespace:     "default",
			expectedError: "unsupported ExtensionRef: configuration.konghq.com/UnsupportedKind",
		},
		{
			name: "successful ExtensionRef fetch",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "test-plugin",
				},
			},
			namespace: "default",
			existingPlugin: &configurationv1.KongPlugin{
				Name:       "test-plugin",
				Namespace:  "default",
				PluginName: "rate-limiting",
			},
			expectedPlugin: &configurationv1.KongPlugin{
				Name:       "test-plugin",
				Namespace:  "default",
				PluginName: "rate-limiting",
			},
		},
		{
			name: "ExtensionRef not found",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "non-existent-plugin",
				},
			},
			namespace:     "default",
			expectedError: "kongplugins.configuration.konghq.com \"non-existent-plugin\" not found",
		},
		{
			name: "ExtensionRef with complex plugin configuration",
			filter: gwtypes.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "complex-plugin",
				},
			},
			namespace: "test-namespace",
			existingPlugin: &configurationv1.KongPlugin{
				Name:       "complex-plugin",
				Namespace:  "test-namespace",
				PluginName: "custom-plugin",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"key":"value"}`),
				},
			},
			expectedPlugin: &configurationv1.KongPlugin{
				Name:       "complex-plugin",
				Namespace:  "test-namespace",
				PluginName: "custom-plugin",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"key":"value"}`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client with the existing plugin if provided
			objects := []client.Object{}
			if tt.existingPlugin != nil {
				objects = append(objects, tt.existingPlugin)
			}
			cl := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objects...).
				Build()

			result, err := getReferencedKongPlugin(context.TODO(), cl, tt.namespace, tt.filter)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedPlugin.Name, result.Name)
				assert.Equal(t, tt.expectedPlugin.Namespace, result.Namespace)
				assert.Equal(t, tt.expectedPlugin.PluginName, result.PluginName)
				if len(tt.expectedPlugin.Config.Raw) > 0 {
					assert.Equal(t, tt.expectedPlugin.Config.Raw, result.Config.Raw)
				}
			}
		})
	}
}

func TestPluginsForRule_ExtensionRef_TagsAnnotation(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta:  httpRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		UID:       "test-uid",
		Annotations: map[string]string{
			pkgmetadata.AnnotationKeyTags: "route-tag",
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	referencedPlugin := &configurationv1.KongPlugin{
		Name:      "referenced-plugin",
		Namespace: "test-namespace",
		Annotations: map[string]string{
			pkgmetadata.AnnotationKeyTags: "plugin-tag,route-tag",
		},
		PluginName: "rate-limiting",
	}

	rule := gwtypes.HTTPRouteRule{
		Filters: []gwtypes.HTTPRouteFilter{
			{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "referenced-plugin",
				},
			},
		},
	}

	fakeClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(referencedPlugin).
		Build()

	plugins, err := PluginsForRule(ctx, logger, fakeClient, httpRoute, rule, parentRef)
	require.NoError(t, err)
	require.Len(t, plugins, 1)

	assert.Equal(t, "plugin-tag,route-tag", plugins[0].Annotations[pkgmetadata.AnnotationKeyTags])
}

func TestPluginsForRule_ExtensionRef_Tags(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta:  httpRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	referencedPlugin := &configurationv1.KongPlugin{
		Name:       "referenced-plugin",
		Namespace:  "test-namespace",
		PluginName: "rate-limiting",
		Tags:       commonv1alpha1.Tags{"team-payments", "env-prod"},
	}

	rule := gwtypes.HTTPRouteRule{
		Filters: []gwtypes.HTTPRouteFilter{
			{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "referenced-plugin",
				},
			},
		},
	}

	fakeClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(referencedPlugin).
		Build()

	plugins, err := PluginsForRule(ctx, logger, fakeClient, httpRoute, rule, parentRef)
	require.NoError(t, err)
	require.Len(t, plugins, 1)

	assert.Equal(t, commonv1alpha1.Tags{"team-payments", "env-prod"}, plugins[0].Tags)
}

// resolvedConfigCase describes a KongPlugin referenced by an ExtensionRef filter together with the
// Secret it sources its configuration from, and the configuration the mirrored copy must carry.
// The cases are shared by the HTTPRoute and GRPCRoute ExtensionRef paths, which resolve the
// configuration the same way.
type resolvedConfigCase struct {
	name        string
	plugin      *configurationv1.KongPlugin
	secret      *corev1.Secret
	expected    string
	expectedErr string
}

// objects returns the cluster state the case needs, for seeding a fake client.
func (c resolvedConfigCase) objects() []client.Object {
	objects := []client.Object{c.plugin}
	if c.secret != nil {
		objects = append(objects, c.secret)
	}
	return objects
}

// resolvedConfigCases returns the shared ExtensionRef configuration resolution cases.
func resolvedConfigCases() []resolvedConfigCase {
	referencedPlugin := func(mutate func(*configurationv1.KongPlugin)) *configurationv1.KongPlugin {
		plugin := &configurationv1.KongPlugin{
			Name:       "referenced-plugin",
			Namespace:  "test-namespace",
			PluginName: "rate-limiting",
		}
		mutate(plugin)
		return plugin
	}
	configSecret := func(data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{
			Name: "plugin-config", Namespace: "test-namespace",
			Data: data,
		}
	}

	return []resolvedConfigCase{
		{
			name: "spec.config is mirrored as-is",
			plugin: referencedPlugin(func(p *configurationv1.KongPlugin) {
				p.Config = apiextensionsv1.JSON{Raw: []byte(`{"minute":10}`)}
			}),
			expected: `{"minute":10}`,
		},
		{
			name: "spec.configFrom is resolved from the Secret",
			plugin: referencedPlugin(func(p *configurationv1.KongPlugin) {
				p.ConfigFrom = &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "plugin-config", Key: "config"},
				}
			}),
			secret:   configSecret(map[string][]byte{"config": []byte("minute: 10\npolicy: local\n")}),
			expected: `{"minute":10,"policy":"local"}`,
		},
		{
			name: "spec.configPatches are applied on top of spec.config",
			plugin: referencedPlugin(func(p *configurationv1.KongPlugin) {
				p.Config = apiextensionsv1.JSON{Raw: []byte(`{"minute":10,"secret":""}`)}
				p.ConfigPatches = []configurationv1.ConfigPatch{{
					Path: "/secret",
					ValueFrom: configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{Secret: "plugin-config", Key: "secret"},
					},
				}}
			}),
			secret:   configSecret(map[string][]byte{"secret": []byte(`"shhh"`)}),
			expected: `{"minute":10,"secret":"shhh"}`,
		},
		{
			name: "a missing Secret surfaces as an error",
			plugin: referencedPlugin(func(p *configurationv1.KongPlugin) {
				p.ConfigFrom = &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "absent", Key: "config"},
				}
			}),
			expectedErr: "plugin configuration secret test-namespace/absent not found: if it exists, it is not matched by --secret-label-selector",
		},
	}
}

func TestPluginsForRule_ExtensionRef_ResolvedConfig(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta:  httpRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		UID:       "test-uid",
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}
	rule := gwtypes.HTTPRouteRule{
		Filters: []gwtypes.HTTPRouteFilter{
			{
				Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "referenced-plugin",
				},
			},
		},
	}

	for _, tc := range resolvedConfigCases() {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.objects()...).
				Build()

			plugins, err := PluginsForRule(ctx, logger, fakeClient, httpRoute, rule, parentRef)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, plugins, 1)
			assert.JSONEq(t, tc.expected, string(plugins[0].Config.Raw))
		})
	}
}
