package pluginbinding

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	httpRouteTypeMeta = metav1.TypeMeta{
		Kind:       "HTTPRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
	}
)

func TestAppendHTTPRouteToBindingAnnotations(t *testing.T) {
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
			binding := &configurationv1alpha1.KongPluginBinding{
				Name:        "test-binding",
				Namespace:   "test-namespace",
				Annotations: tt.existingAnnotations,
			}

			am := metadata.NewAnnotationManager(logger)
			am.AppendRouteToAnnotation(binding, tt.httpRoute)
			actualAnnotation := binding.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestBindingForPluginAndRoute(t *testing.T) {
	logger := logr.Discard()
	ctx := context.Background()

	tests := []struct {
		name            string
		pluginName      string
		routeName       string
		existingBinding *configurationv1alpha1.KongPluginBinding
		httpRoute       *gwtypes.HTTPRoute
		parentRef       *gwtypes.ParentReference
		cpRef           *commonv1alpha1.ControlPlaneRef
		expectedError   bool
		validateBinding func(t *testing.T, binding *configurationv1alpha1.KongPluginBinding)
	}{
		{
			name:            "create new binding",
			pluginName:      "test-plugin",
			routeName:       "test-route",
			existingBinding: nil,
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-httproute",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			cpRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectedError: false,
			validateBinding: func(t *testing.T, binding *configurationv1alpha1.KongPluginBinding) {
				require.NotNil(t, binding)
				assert.Equal(t, "test-namespace", binding.Namespace)
				assert.Equal(t, "test-plugin", binding.Spec.PluginReference.Name)
				assert.NotNil(t, binding.Spec.Targets)
				assert.NotNil(t, binding.Spec.Targets.RouteReference)
				assert.Equal(t, "test-route", binding.Spec.Targets.RouteReference.Name)
				assert.Equal(t, "configuration.konghq.com", binding.Spec.Targets.RouteReference.Group)
				assert.Equal(t, "KongRoute", binding.Spec.Targets.RouteReference.Kind)
				assert.Contains(t, binding.Annotations, consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation)
				assert.Equal(t, "test-namespace/test-httproute", binding.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
			},
		},
		{
			name:       "binding with control plane ref",
			pluginName: "test-plugin-2",
			routeName:  "test-route-2",
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta:  httpRouteTypeMeta,
				Name:      "test-httproute-2",
				Namespace: "test-namespace",
				UID:       "test-uid-2",
			},
			parentRef: &gwtypes.ParentReference{
				Name: "test-gateway",
			},
			cpRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectedError: false,
			validateBinding: func(t *testing.T, binding *configurationv1alpha1.KongPluginBinding) {
				require.NotNil(t, binding)
				assert.Equal(t, commonv1alpha1.ControlPlaneRefKonnectNamespacedRef, binding.Spec.ControlPlaneRef.Type)
				assert.NotNil(t, binding.Spec.ControlPlaneRef.KonnectNamespacedRef)
				assert.Equal(t, "test-cp", binding.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := scheme.Get()
			var objects []runtime.Object
			if tt.existingBinding != nil {
				objects = append(objects, tt.existingBinding)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objects...).
				Build()

			binding, err := BindingForPluginAndRoute(
				ctx,
				logger,
				fakeClient,
				tt.httpRoute,
				tt.parentRef,
				tt.cpRef,
				tt.pluginName,
				tt.routeName,
			)

			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.validateBinding != nil {
				tt.validateBinding(t, binding)
			}
		})
	}
}

// TestBindingsForPlugin_InheritedTags pins that a KongPluginBinding inherits the tags of the
// parent Gateway it was generated for, and of that Gateway's GatewayClass. A binding is scoped to
// one route and one parentRef, so the route's other parents must not contribute.
func TestBindingsForPlugin_InheritedTags(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	newGateway := func(name, class, tags string) *gwtypes.Gateway {
		return &gwtypes.Gateway{
			Name:        name,
			Namespace:   "test-namespace",
			Annotations: map[string]string{"konghq.com/tags": tags},
			Spec:        gwtypes.GatewaySpec{GatewayClassName: gwtypes.ObjectName(class)},
		}
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(
		newGateway("gw-a", "class-a", "team-a"),
		newGateway("gw-b", "class-b", "team-b"),
		&gwtypes.GatewayClass{Name: "class-a", Annotations: map[string]string{"konghq.com/tags": "class-a-tag"}},
		&gwtypes.GatewayClass{Name: "class-b", Annotations: map[string]string{"konghq.com/tags": "class-b-tag"}},
	).Build()

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta:  httpRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		Spec: gwtypes.HTTPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{Name: "gw-a"}, {Name: "gw-b"}},
			},
		},
	}
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}

	tests := []struct {
		parentRef string
		expected  commonv1alpha1.Tags
	}{
		{parentRef: "gw-a", expected: commonv1alpha1.Tags{"team-a", "class-a-tag"}},
		{parentRef: "gw-b", expected: commonv1alpha1.Tags{"team-b", "class-b-tag"}},
		{parentRef: "ghost", expected: nil},
	}
	for _, tt := range tests {
		t.Run(tt.parentRef, func(t *testing.T) {
			pRef := &gwtypes.ParentReference{Name: gwtypes.ObjectName(tt.parentRef)}

			routeBinding, err := BindingForPluginAndRoute(ctx, logger, fakeClient, httpRoute, pRef, cpRef, "test-plugin", "test-kong-route")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, routeBinding.Spec.Tags)

			serviceBinding, err := BindingForPluginAndService(ctx, logger, fakeClient, httpRoute, pRef, cpRef, "test-plugin", "test-kong-service")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, serviceBinding.Spec.Tags)
		})
	}
}
