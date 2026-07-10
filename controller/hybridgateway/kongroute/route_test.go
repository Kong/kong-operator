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
	routebuilder "github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
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
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &prefix,
					Value: new("/"),
				},
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
		expectProtocols []sdkkonnectcomp.Protocols
	}{
		{
			name:         "no sectionName - both protocols from all listeners",
			parentRef:    pRef,
			serviceName:  "test-service",
			hostnames:    []string{"example.com"},
			expectRoutes: 2,
			expectProtocols: []sdkkonnectcomp.Protocols{
				sdkkonnectcomp.Protocols("http"),
				sdkkonnectcomp.Protocols("https"),
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
			expectProtocols: []sdkkonnectcomp.Protocols{
				sdkkonnectcomp.Protocols("http"),
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
			expectProtocols: []sdkkonnectcomp.Protocols{
				sdkkonnectcomp.Protocols("https"),
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

			results, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, rule, 0, tt.parentRef, cpRef, nil, tt.serviceName, tt.hostnames)

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

				if len(result.Spec.Headers) > 0 {
					assert.Contains(t, result.Spec.Headers, "X-Foo")
					assert.Equal(t, []string{routebuilder.KongHTTPRouteHeaderOnlyRegexPath}, result.Spec.Paths)
					require.NotNil(t, result.Spec.RegexPriority)
					assert.GreaterOrEqual(t, *result.Spec.RegexPriority, int64(0))
					continue
				}

				// Verify path-only route.
				require.NotNil(t, result.Spec.RegexPriority)
				assert.Equal(t, routebuilder.KongHTTPRoutePathRegexPriorityOffset+10, *result.Spec.RegexPriority)
			}
		})
	}
}

func TestRoutesForRule_PrioritizesHeaderOnlyHTTPRouteMatches(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, gatewayv1.Install(scheme))

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: httpRouteTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "header-matching",
			Namespace: "gateway-conformance-infra",
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{Name: "same-namespace"}},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path:    defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{{Name: "version", Value: "one"}},
					}},
				},
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path:    defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{{Name: "version", Value: "two"}},
					}},
				},
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path: defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{
							{Name: "version", Value: "two"},
							{Name: "color", Value: "orange"},
						},
					}},
				},
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path:    defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{{Name: "color", Value: "blue"}},
					}},
				},
			},
		},
	}
	pRef := &gwtypes.ParentReference{
		Name:      "same-namespace",
		Namespace: (*gatewayv1.Namespace)(new("gateway-conformance-infra")),
	}
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "same-namespace",
			Namespace: "gateway-conformance-infra",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gatewayv1.Listener{
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway).Build()

	versionTwoRoutes, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, httpRoute.Spec.Rules[1], 1, pRef, cpRef, nil, "version-two-service", nil)
	require.NoError(t, err)
	require.Len(t, versionTwoRoutes, 1)
	twoHeaderRoutes, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, httpRoute.Spec.Rules[2], 2, pRef, cpRef, nil, "two-header-service", nil)
	require.NoError(t, err)
	require.Len(t, twoHeaderRoutes, 1)
	colorBlueRoutes, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, httpRoute.Spec.Rules[3], 3, pRef, cpRef, nil, "color-blue-service", nil)
	require.NoError(t, err)
	require.Len(t, colorBlueRoutes, 1)

	versionTwoRoute := versionTwoRoutes[0]
	twoHeaderRoute := twoHeaderRoutes[0]
	colorBlueRoute := colorBlueRoutes[0]
	require.NotNil(t, versionTwoRoute.Spec.RegexPriority)
	require.NotNil(t, twoHeaderRoute.Spec.RegexPriority)
	require.NotNil(t, colorBlueRoute.Spec.RegexPriority)
	assert.Greater(t, *twoHeaderRoute.Spec.RegexPriority, *versionTwoRoute.Spec.RegexPriority)
	assert.Greater(t, *versionTwoRoute.Spec.RegexPriority, *colorBlueRoute.Spec.RegexPriority)
	assert.GreaterOrEqual(t, *twoHeaderRoute.Spec.RegexPriority, int64(0))
	assert.GreaterOrEqual(t, *versionTwoRoute.Spec.RegexPriority, int64(0))
	assert.GreaterOrEqual(t, *colorBlueRoute.Spec.RegexPriority, int64(0))
	assert.Less(t, *twoHeaderRoute.Spec.RegexPriority, routebuilder.KongHTTPRoutePathRegexPriorityOffset)
	assert.Equal(t, []string{routebuilder.KongHTTPRouteHeaderOnlyRegexPath}, versionTwoRoute.Spec.Paths)
	assert.Equal(t, []string{routebuilder.KongHTTPRouteHeaderOnlyRegexPath}, twoHeaderRoute.Spec.Paths)
	assert.Equal(t, []string{routebuilder.KongHTTPRouteHeaderOnlyRegexPath}, colorBlueRoute.Spec.Paths)
}

func TestHTTPRouteMatchPrioritiesIgnoreUnsupportedQueryParams(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		Spec: gatewayv1.HTTPRouteSpec{
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path: defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{{
							Name:  "version",
							Value: "two",
						}},
					}},
				},
				{
					Matches: []gatewayv1.HTTPRouteMatch{{
						Path:    defaultRootPathMatch(),
						Headers: []gatewayv1.HTTPHeaderMatch{{Name: "version", Value: "two"}},
						QueryParams: []gatewayv1.HTTPQueryParamMatch{{
							Name:  "animal",
							Value: "whale",
						}},
					}},
				},
			},
		},
	}

	priorities := httpRouteMatchPriorities(httpRoute)

	assert.Greater(t, priorityForHTTPRouteMatch(priorities, 0, 0), priorityForHTTPRouteMatch(priorities, 1, 0))
}

func defaultRootPathMatch() *gatewayv1.HTTPPathMatch {
	return &gatewayv1.HTTPPathMatch{
		Type:  new(gatewayv1.PathMatchPathPrefix),
		Value: new("/"),
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

	results, err := RoutesForRule(ctx, logger, fakeClient, httpRoute, rule, 0, pRef, cpRef, nil, "test-service", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, []string{"~/one$"}, results[0].Spec.Paths)
	require.NotNil(t, results[0].Spec.RegexPriority)
	assert.Equal(t, routebuilder.KongHTTPRoutePathRegexPriorityOffset+9, *results[0].Spec.RegexPriority)
	assert.ElementsMatch(t,
		[]sdkkonnectcomp.Protocols{
			sdkkonnectcomp.Protocols("http"),
			sdkkonnectcomp.Protocols("https"),
		},
		results[0].Spec.Protocols,
	)
}

func TestRoutesForHTTPRouteRule_MalformedAnnotations(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, gatewayv1.Install(scheme))

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
	rule := gwtypes.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{{
			Path: &gatewayv1.HTTPPathMatch{
				Type:  new(gatewayv1.PathMatchPathPrefix),
				Value: new("/test"),
			},
		}},
	}
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gateway", Namespace: "test-namespace"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-class",
			Listeners: []gatewayv1.Listener{
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway).Build()

	tests := []struct {
		name        string
		annotations map[string]string
	}{
		{
			name: "strip-path",
			annotations: map[string]string{
				"konghq.com/strip-path": "not-a-bool",
			},
		},
		{
			name: "preserve-host",
			annotations: map[string]string{
				"konghq.com/preserve-host": "not-a-bool",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRoute := &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-route",
					Namespace:   "test-namespace",
					Annotations: tt.annotations,
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{Name: "test-gateway"}},
					},
				},
			}

			routes, err := RoutesForHTTPRouteRule(ctx, logger, fakeClient, httpRoute, rule, 0, pRef, cpRef, nil, "test-service", nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, hgerrors.ErrMalformedAnnotation)
			assert.Nil(t, routes)
			assert.Contains(t, err.Error(), tt.name)
			assert.Contains(t, err.Error(), "test-namespace/test-route")
		})
	}
}
