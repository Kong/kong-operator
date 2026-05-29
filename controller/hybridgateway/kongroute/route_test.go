package kongroute

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var httpRouteTypeMeta = metav1.TypeMeta{
	Kind:       "HTTPRoute",
	APIVersion: "gateway.networking.k8s.io/v1",
}

func TestRoutesForRule(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create a scheme with the necessary types
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, gatewayv1.Install(scheme))

	// Create test HTTPRoute
	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "test-gateway"},
				},
			},
		},
	}

	// Create test rule with two matches so that two KongRoutes are generated
	prefix := gatewayv1.PathMatchPathPrefix
	rule := gwtypes.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{
			{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &prefix,
					Value: new("/test"),
				},
			},
			{
				Headers: []gatewayv1.HTTPHeaderMatch{{
					Name:  "X-Foo",
					Value: "bar",
				}},
			},
		},
	}

	// Create test parent reference
	pRef := &gwtypes.ParentReference{
		Name:      "test-gateway",
		Namespace: (*gatewayv1.Namespace)(new("test-namespace")),
	}

	// Create test control plane reference
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}

	// Gateway with both HTTP and HTTPS listeners.
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "test-namespace",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gatewayv1.Listener{
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
				{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
			},
		},
	}

	httpSectionName := gatewayv1.SectionName("http")
	httpsSectionName := gatewayv1.SectionName("https")

	tests := []struct {
		name            string
		existingRoute   *configurationv1alpha1.KongRoute
		parentRef       *gwtypes.ParentReference
		serviceName     string
		hostnames       []string
		expectError     bool
		expectRoutes    int
		expectProtocols []sdkkonnectcomp.RouteJSONProtocols
	}{
		{
			name:         "no sectionName - both protocols from all listeners",
			parentRef:    pRef,
			serviceName:  "test-service",
			hostnames:    []string{"example.com"},
			expectRoutes: 2,
			expectProtocols: []sdkkonnectcomp.RouteJSONProtocols{
				sdkkonnectcomp.RouteJSONProtocols("http"),
				sdkkonnectcomp.RouteJSONProtocols("https"),
			},
		},
		{
			name: "sectionName=http - only http protocol",
			parentRef: &gwtypes.ParentReference{
				Name:        "test-gateway",
				Namespace:   (*gatewayv1.Namespace)(new("test-namespace")),
				SectionName: &httpSectionName,
			},
			serviceName:  "test-service",
			hostnames:    []string{"example.com"},
			expectRoutes: 2,
			expectProtocols: []sdkkonnectcomp.RouteJSONProtocols{
				sdkkonnectcomp.RouteJSONProtocols("http"),
			},
		},
		{
			name: "sectionName=https - only https protocol",
			parentRef: &gwtypes.ParentReference{
				Name:        "test-gateway",
				Namespace:   (*gatewayv1.Namespace)(new("test-namespace")),
				SectionName: &httpsSectionName,
			},
			serviceName:  "test-service",
			hostnames:    []string{"example.com"},
			expectRoutes: 2,
			expectProtocols: []sdkkonnectcomp.RouteJSONProtocols{
				sdkkonnectcomp.RouteJSONProtocols("https"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			objects = append(objects, gateway)
			if tt.existingRoute != nil {
				objects = append(objects, tt.existingRoute)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			results, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, rule, tt.parentRef, cpRef, nil, tt.serviceName, tt.hostnames)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, results)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, results)
			assert.Len(t, results, tt.expectRoutes)

			for _, result := range results {
				// Verify the route has correct properties
				assert.Equal(t, "test-namespace", result.Namespace)
				assert.NotEmpty(t, result.Name)
				assert.Equal(t, tt.hostnames, result.Spec.Hosts)
				assert.ElementsMatch(t, tt.expectProtocols, result.Spec.Protocols)

				// Verify service reference
				if tt.serviceName != "" {
					require.NotNil(t, result.Spec.ServiceRef)
					assert.Equal(t, configurationv1alpha1.ServiceRefNamespacedRef, result.Spec.ServiceRef.Type)
					require.NotNil(t, result.Spec.ServiceRef.NamespacedRef)
					assert.Equal(t, tt.serviceName, result.Spec.ServiceRef.NamespacedRef.Name)
				}

				// Verify HTTPRoute annotation
				expectedAnnotation := httpRoute.Namespace + "/" + httpRoute.Name
				assert.Contains(t, result.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation], expectedAnnotation)

				// Verify at least one path/header/method is set based on the match.
				// For the first match with path, we expect Paths to be non-empty.
				// For the second header-only match, Paths may be empty; Headers must be set.
				if len(result.Spec.Paths) == 0 {
					// header-only route
					assert.Contains(t, result.Spec.Headers, "X-Foo")
					assert.Nil(t, result.Spec.RegexPriority)
					continue
				}

				assert.Equal(t, new(int64(1)), result.Spec.RegexPriority)
			}
		})
	}
}

func TestRoutesForRule_ExactPathMatch(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, gatewayv1.Install(scheme))

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{Name: "test-gateway"},
				},
			},
		},
	}

	exact := gatewayv1.PathMatchExact
	rule := gwtypes.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  &exact,
				Value: new("/one"),
			},
		}},
	}

	pRef := &gwtypes.ParentReference{
		Name:      "test-gateway",
		Namespace: (*gatewayv1.Namespace)(new("test-namespace")),
	}

	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}

	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "test-namespace",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gatewayv1.Listener{
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
				{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway).Build()

	results, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, rule, pRef, cpRef, nil, "test-service", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, []string{"~/one$"}, results[0].Spec.Paths)
	assert.Equal(t, new(int64(1)), results[0].Spec.RegexPriority)
	assert.ElementsMatch(t,
		[]sdkkonnectcomp.RouteJSONProtocols{
			sdkkonnectcomp.RouteJSONProtocols("http"),
			sdkkonnectcomp.RouteJSONProtocols("https"),
		},
		results[0].Spec.Protocols,
	)
}
