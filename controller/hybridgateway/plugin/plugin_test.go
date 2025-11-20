package plugin

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "empty hybrid-routes annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "existing different route in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route,test-namespace/test-route",
			expectModification: true,
		},
		{
			name: "route already exists in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectModification: false,
		},
		{
			name: "multiple existing routes, adding new one",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route3",
					Namespace: "ns3",
				},
			},
			expectedAnnotation: "ns1/route1,ns2/route2,ns3/route3",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-plugin",
					Namespace:   "test-namespace",
					Annotations: tt.existingAnnotations,
				},
			}

			am := metadata.NewAnnotationManager(logger)
			am.AppendRouteToAnnotation(plugin, tt.httpRoute)
			actualAnnotation := plugin.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
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
			existingPlugin: nil,
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
					UID:       "test-uid",
				},
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			expectedError: false,
			validatePlugin: func(t *testing.T, plugin *configurationv1.KongPlugin) {
				require.NotNil(t, plugin)
				assert.Equal(t, "test-namespace", plugin.Namespace)
				assert.Equal(t, "request-transformer", plugin.PluginName)
				assert.Contains(t, plugin.Annotations, consts.GatewayOperatorHybridRoutesAnnotation)
				assert.Equal(t, "test-namespace/test-route", plugin.Annotations[consts.GatewayOperatorHybridRoutesAnnotation])
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
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			plugin, err := PluginForFilter(ctx, logger, fakeClient, tt.httpRoute, tt.filter, tt.parentRef)

			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.validatePlugin != nil {
				tt.validatePlugin(t, plugin)
			}
		})
	}
}
