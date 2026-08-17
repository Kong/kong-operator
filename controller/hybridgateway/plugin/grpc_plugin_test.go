package plugin

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	pkgmetadata "github.com/kong/kong-operator/v2/pkg/metadata"
)

var grpcRouteTypeMeta = metav1.TypeMeta{
	Kind:       "GRPCRoute",
	APIVersion: "gateway.networking.k8s.io/v1",
}

func TestGRPCPluginsForRule(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta: grpcRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
			Annotations: map[string]string{
				pkgmetadata.AnnotationKeyTags: "grpc-tag, shared-tag",
			},
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	rule := gwtypes.GRPCRouteRule{
		Filters: []gatewayv1.GRPCRouteFilter{
			{
				Type: gatewayv1.GRPCRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
					Set: []gatewayv1.HTTPHeader{
						{Name: "X-Custom-Header", Value: "custom-value"},
					},
				},
			},
		},
	}

	fakeClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		Build()

	plugins, err := GRPCPluginsForRule(ctx, logger, fakeClient, grpcRoute, rule, parentRef)
	require.NoError(t, err)
	require.Len(t, plugins, 1)

	plugin := plugins[0]
	assert.Equal(t, "test-namespace", plugin.Namespace)
	assert.Equal(t, "request-transformer", plugin.PluginName)
	assert.Contains(t, plugin.Annotations, consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation)
	assert.Equal(t, "test-namespace/test-route", plugin.Annotations[consts.GatewayOperatorHybridRoutesGRPCRouteAnnotation])
	assert.Equal(t, "grpc-tag,shared-tag", plugin.Annotations[pkgmetadata.AnnotationKeyTags])
}

func TestGRPCPluginsForRule_UnsupportedFilterErrors(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta: grpcRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}
	rule := gwtypes.GRPCRouteRule{
		Filters: []gatewayv1.GRPCRouteFilter{
			{Type: gatewayv1.GRPCRouteFilterRequestMirror},
		},
	}

	fakeClient := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()

	_, err := GRPCPluginsForRule(ctx, logger, fakeClient, grpcRoute, rule, &gwtypes.ParentReference{Name: "gw"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported filter type")
}

func TestGetReferencedKongPluginForGRPCFilter(t *testing.T) {
	tests := []struct {
		name           string
		filter         gatewayv1.GRPCRouteFilter
		namespace      string
		existingPlugin *configurationv1.KongPlugin
		expectedPlugin *configurationv1.KongPlugin
		expectedError  string
	}{
		{
			name: "nil ExtensionRef",
			filter: gatewayv1.GRPCRouteFilter{
				Type:         gatewayv1.GRPCRouteFilterExtensionRef,
				ExtensionRef: nil,
			},
			namespace:     "default",
			expectedError: "ExtensionRef filter is missing",
		},
		{
			name: "unsupported ExtensionRef group",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "test-plugin",
				},
			},
			namespace: "default",
			existingPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-plugin",
					Namespace: "default",
				},
				PluginName: "rate-limiting",
			},
			expectedPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-plugin",
					Namespace: "default",
				},
				PluginName: "rate-limiting",
			},
		},
		{
			name: "ExtensionRef not found",
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
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
			filter: gatewayv1.GRPCRouteFilter{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(configurationv1.GroupVersion.Group),
					Kind:  "KongPlugin",
					Name:  "complex-plugin",
				},
			},
			namespace: "test-namespace",
			existingPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-plugin",
					Namespace: "test-namespace",
				},
				PluginName: "custom-plugin",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"key":"value"}`),
				},
			},
			expectedPlugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "complex-plugin",
					Namespace: "test-namespace",
				},
				PluginName: "custom-plugin",
				Config: apiextensionsv1.JSON{
					Raw: []byte(`{"key":"value"}`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			if tt.existingPlugin != nil {
				objects = append(objects, tt.existingPlugin)
			}
			fakeClient := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithRuntimeObjects(objects...).
				Build()

			plugin, err := getReferencedKongPluginForGRPCFilter(context.Background(), fakeClient, tt.namespace, tt.filter)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedPlugin.Name, plugin.Name)
			assert.Equal(t, tt.expectedPlugin.Namespace, plugin.Namespace)
			assert.Equal(t, tt.expectedPlugin.PluginName, plugin.PluginName)
			assert.Equal(t, tt.expectedPlugin.Config.Raw, plugin.Config.Raw)
		})
	}
}

func TestGRPCPluginsForRule_ExtensionRef_TagsAnnotation(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	grpcRoute := &gwtypes.GRPCRoute{
		TypeMeta: grpcRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
			Annotations: map[string]string{
				pkgmetadata.AnnotationKeyTags: "route-tag",
			},
		},
	}
	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	referencedPlugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "referenced-plugin",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				pkgmetadata.AnnotationKeyTags: "plugin-tag,route-tag",
			},
		},
		PluginName: "rate-limiting",
	}

	rule := gwtypes.GRPCRouteRule{
		Filters: []gatewayv1.GRPCRouteFilter{
			{
				Type: gatewayv1.GRPCRouteFilterExtensionRef,
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

	plugins, err := GRPCPluginsForRule(ctx, logger, fakeClient, grpcRoute, rule, parentRef)
	require.NoError(t, err)
	require.Len(t, plugins, 1)

	assert.Equal(t, "plugin-tag,route-tag", plugins[0].Annotations[pkgmetadata.AnnotationKeyTags])
}
