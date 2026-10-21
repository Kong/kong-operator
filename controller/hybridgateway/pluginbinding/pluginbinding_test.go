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
	"github.com/kong/kong-operator/v2/pkg/vars"
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

// TestBindingsForPlugin_InheritedTags pins the two binding kinds' different scoping.
//
// A per-KongRoute binding is named after its KongRoute, which is named per parentRef, so it takes
// the tags of the one parent Gateway it was generated for.
//
// The per-KongService binding is not: it is named after the backend-not-found KongService, whose
// name has no parentRef in it, so a route attached to several Gateways on one ControlPlane builds
// the same binding once per parent and the converter keeps only the first. It must therefore carry
// the same tags whichever parent built it - the union over every parent this controller accepted -
// or the tags of all but one Gateway would be silently dropped.
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
		newGateway("theirs", "their-class", "their-tag"),
		&gwtypes.GatewayClass{Name: "class-a", Annotations: map[string]string{"konghq.com/tags": "class-a-tag"}},
		&gwtypes.GatewayClass{Name: "class-b", Annotations: map[string]string{"konghq.com/tags": "class-b-tag"}},
		&gwtypes.GatewayClass{Name: "their-class", Annotations: map[string]string{"konghq.com/tags": "their-class-tag"}},
	).Build()

	accepted := func(name string) gwtypes.RouteParentStatus {
		return gwtypes.RouteParentStatus{
			ParentRef:      gwtypes.ParentReference{Name: gwtypes.ObjectName(name)},
			ControllerName: gwtypes.GatewayController(vars.ControllerName()),
		}
	}
	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta:  httpRouteTypeMeta,
		Name:      "test-route",
		Namespace: "test-namespace",
		Spec: gwtypes.HTTPRouteSpec{
			CommonRouteSpec: gwtypes.CommonRouteSpec{
				ParentRefs: []gwtypes.ParentReference{{Name: "gw-a"}, {Name: "gw-b"}, {Name: "theirs"}},
			},
		},
	}
	// gw-a and gw-b are ours; "theirs" belongs to another implementation. Translation only runs on
	// a route whose status is settled, so the fixture carries it.
	httpRoute.Status.Parents = []gwtypes.RouteParentStatus{
		accepted("gw-a"),
		accepted("gw-b"),
		{
			ParentRef:      gwtypes.ParentReference{Name: "theirs"},
			ControllerName: "example.com/some-other-controller",
		},
	}
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}

	t.Run("per-KongRoute binding takes its own parent's tags", func(t *testing.T) {
		for _, tt := range []struct {
			parentRef string
			expected  commonv1alpha1.Tags
		}{
			{parentRef: "gw-a", expected: commonv1alpha1.Tags{"team-a", "class-a-tag"}},
			{parentRef: "gw-b", expected: commonv1alpha1.Tags{"team-b", "class-b-tag"}},
			{parentRef: "ghost", expected: nil},
		} {
			t.Run(tt.parentRef, func(t *testing.T) {
				pRef := &gwtypes.ParentReference{Name: gwtypes.ObjectName(tt.parentRef)}
				binding, err := BindingForPluginAndRoute(ctx, logger, fakeClient, httpRoute, pRef, cpRef, "test-plugin", "test-kong-route-"+tt.parentRef)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, binding.Spec.Tags)
			})
		}
	})

	t.Run("per-KongService binding carries every accepted parent's tags whichever parent built it", func(t *testing.T) {
		// Every Gateway tag precedes every GatewayClass tag; the unmanaged Gateway contributes nothing.
		want := commonv1alpha1.Tags{"team-a", "team-b", "class-a-tag", "class-b-tag"}

		var names []string
		for _, parent := range []string{"gw-a", "gw-b"} {
			pRef := &gwtypes.ParentReference{Name: gwtypes.ObjectName(parent)}
			binding, err := BindingForPluginAndService(ctx, logger, fakeClient, httpRoute, pRef, cpRef, "test-plugin", "test-kong-service")
			require.NoError(t, err)
			assert.Equal(t, want, binding.Spec.Tags, "built for parent %s", parent)
			names = append(names, binding.Name)
		}
		// This is why the tags must not depend on the parent: both parents produce one object.
		require.Equal(t, names[0], names[1], "both parents must build the same binding")
	})
}
