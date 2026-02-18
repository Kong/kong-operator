package builder

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestKongRouteBuilder_NewKongRoute(t *testing.T) {
	builder := NewKongRoute()

	assert.NotNil(t, builder)
	assert.NotNil(t, builder.errors)
	assert.Empty(t, builder.errors)
	assert.Equal(t, configurationv1alpha1.KongRoute{}, builder.route)
}

func TestKongRouteBuilder_WithHosts(t *testing.T) {
	tests := []struct {
		name     string
		hosts    []string
		expected []string
	}{
		{
			name:     "single host",
			hosts:    []string{"example.com"},
			expected: []string{"example.com"},
		},
		{
			name:     "multiple hosts",
			hosts:    []string{"example.com", "api.example.com"},
			expected: []string{"example.com", "api.example.com"},
		},
		{
			name:     "empty hosts",
			hosts:    []string{},
			expected: nil,
		},
		{
			name:     "nil hosts",
			hosts:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongRoute().WithHosts(tt.hosts)

			route, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, route.Spec.Hosts)
		})
	}
}

func TestKongRouteBuilder_WithHosts_Chainable(t *testing.T) {
	builder := NewKongRoute().
		WithHosts([]string{"example.com"}).
		WithHosts([]string{"api.example.com", "test.example.com"})

	route, err := builder.Build()
	require.NoError(t, err)
	expected := []string{"example.com", "api.example.com", "test.example.com"}
	assert.Equal(t, expected, route.Spec.Hosts)
}

func TestKongRouteBuilder_WithHTTPRouteMatch(t *testing.T) {
	pathType := gatewayv1.PathMatchPathPrefix
	pathValue := "/api"
	method := gatewayv1.HTTPMethodGet

	tests := []struct {
		name     string
		match    gwtypes.HTTPRouteMatch
		validate func(t *testing.T, route configurationv1alpha1.KongRoute)
	}{
		{
			name: "with path",
			match: gwtypes.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: &pathValue,
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Equal(t, []string{"~/api$", "/api/"}, route.Spec.Paths)
				assert.Empty(t, route.Spec.Methods)
				assert.Nil(t, route.Spec.Headers)
			},
		},
		{
			name: "with exact path",
			match: gwtypes.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  lo.ToPtr(gatewayv1.PathMatchExact),
					Value: &pathValue,
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Equal(t, []string{"~/api$"}, route.Spec.Paths)
				assert.Empty(t, route.Spec.Methods)
				assert.Nil(t, route.Spec.Headers)
			},
		},
		{
			name: "with regular expression path",
			match: gwtypes.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  lo.ToPtr(gatewayv1.PathMatchRegularExpression),
					Value: &pathValue,
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Equal(t, []string{"~/api"}, route.Spec.Paths)
				assert.Empty(t, route.Spec.Methods)
				assert.Nil(t, route.Spec.Headers)
			},
		},
		{
			name: "with method",
			match: gwtypes.HTTPRouteMatch{
				Method: &method,
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Empty(t, route.Spec.Paths)
				assert.Equal(t, []string{"GET"}, route.Spec.Methods)
				assert.Nil(t, route.Spec.Headers)
			},
		},
		{
			name: "with headers",
			match: gwtypes.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  lo.ToPtr(gatewayv1.HeaderMatchExact),
						Name:  "Authorization",
						Value: "Bearer token",
					},
					{
						Type:  nil,
						Name:  "Content-Type",
						Value: "application/json",
					},
					{
						Type:  lo.ToPtr(gatewayv1.HeaderMatchRegularExpression),
						Name:  "Foo",
						Value: "(bar|baz)",
					},
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Empty(t, route.Spec.Paths)
				assert.Empty(t, route.Spec.Methods)
				require.NotNil(t, route.Spec.Headers)
				assert.Equal(t, []string{"Bearer token"}, route.Spec.Headers["Authorization"])
				assert.Equal(t, []string{"application/json"}, route.Spec.Headers["Content-Type"])
				assert.Equal(t, []string{"~*(bar|baz)"}, route.Spec.Headers["Foo"])
			},
		},
		{
			name: "with multiple headers same name",
			match: gwtypes.HTTPRouteMatch{
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  lo.ToPtr(gatewayv1.HeaderMatchExact),
						Name:  "Accept",
						Value: "application/json",
					},
					{
						Type:  lo.ToPtr(gatewayv1.HeaderMatchExact),
						Name:  "Accept",
						Value: "text/plain",
					},
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				require.NotNil(t, route.Spec.Headers)
				assert.Equal(t, []string{"application/json", "text/plain"}, route.Spec.Headers["Accept"])
			},
		},
		{
			name: "complete match",
			match: gwtypes.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: &pathValue,
				},
				Method: &method,
				Headers: []gatewayv1.HTTPHeaderMatch{
					{
						Type:  lo.ToPtr(gatewayv1.HeaderMatchExact),
						Name:  "Authorization",
						Value: "Bearer token",
					},
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Equal(t, []string{"~/api$", "/api/"}, route.Spec.Paths)
				assert.Equal(t, []string{"GET"}, route.Spec.Methods)
				require.NotNil(t, route.Spec.Headers)
				assert.Equal(t, []string{"Bearer token"}, route.Spec.Headers["Authorization"])
			},
		},
		{
			name: "nil path value",
			match: gwtypes.HTTPRouteMatch{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  &pathType,
					Value: nil,
				},
			},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Empty(t, route.Spec.Paths)
			},
		},
		{
			name:  "empty match",
			match: gwtypes.HTTPRouteMatch{},
			validate: func(t *testing.T, route configurationv1alpha1.KongRoute) {
				assert.Empty(t, route.Spec.Paths)
				assert.Empty(t, route.Spec.Methods)
				assert.Nil(t, route.Spec.Headers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongRoute().WithHTTPRouteMatch(tt.match, false)

			route, err := builder.Build()
			require.NoError(t, err)
			tt.validate(t, route)
		})
	}
}

func TestKongRouteBuilder_WithKongService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected *configurationv1alpha1.ServiceRef
	}{
		{
			name:    "with service name",
			service: "test-service",
			expected: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "test-service",
				},
			},
		},
		{
			name:     "empty service name",
			service:  "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongRoute().WithKongService(tt.service)

			route, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, route.Spec.ServiceRef)
		})
	}
}

func TestKongRouteBuilder_WithSpecName(t *testing.T) {
	tests := []struct {
		name     string
		specName string
		expected *string
	}{
		{
			name:     "with spec name",
			specName: "test-route-spec",
			expected: &[]string{"test-route-spec"}[0],
		},
		{
			name:     "empty spec name",
			specName: "",
			expected: &[]string{""}[0],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongRoute().WithSpecName(tt.specName)

			route, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, route.Spec.Name)
		})
	}
}

func TestKongRouteBuilder_WithStripPath(t *testing.T) {
	tests := []struct {
		name      string
		stripPath bool
		expected  *bool
	}{
		{
			name:      "strip path true",
			stripPath: true,
			expected:  &[]bool{true}[0],
		},
		{
			name:      "strip path false",
			stripPath: false,
			expected:  &[]bool{false}[0],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewKongRoute().WithStripPath(tt.stripPath)

			route, err := builder.Build()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, route.Spec.StripPath)
		})
	}
}

func TestKongRouteBuilder_WithName(t *testing.T) {
	builder := NewKongRoute().WithName("test-route")

	route, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-route", route.Name)
}

func TestKongRouteBuilder_WithNamespace(t *testing.T) {
	builder := NewKongRoute().WithNamespace("test-namespace")

	route, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, "test-namespace", route.Namespace)
}

func TestKongRouteBuilder_WithOwner(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	t.Run("valid owner", func(t *testing.T) {
		builder := NewKongRoute().
			WithNamespace("test-namespace").
			WithOwner(httpRoute)

		route, err := builder.Build()
		require.NoError(t, err)

		require.Len(t, route.OwnerReferences, 1)
		ownerRef := route.OwnerReferences[0]
		assert.Equal(t, "test-http-route", ownerRef.Name)
		assert.Equal(t, "test-uid", string(ownerRef.UID))
		assert.True(t, *ownerRef.BlockOwnerDeletion)
	})

	t.Run("nil owner", func(t *testing.T) {
		builder := NewKongRoute().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})

	t.Run("owner reference error", func(t *testing.T) {
		httpRouteWithoutTypeMeta := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-http-route",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
		}

		builder := NewKongRoute().WithOwner(httpRouteWithoutTypeMeta)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set owner reference")
	})
}

func TestKongRouteBuilder_WithLabels(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongRoute().WithLabels(httpRoute, parentRef)

	route, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, route.Labels)
	assert.NotEmpty(t, route.Labels)
}

func TestKongRouteBuilder_WithAnnotations(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	builder := NewKongRoute().WithAnnotations(httpRoute, parentRef)

	route, err := builder.Build()
	require.NoError(t, err)

	assert.NotNil(t, route.Annotations)
}

func TestKongRouteBuilder_Build(t *testing.T) {
	t.Run("successful build", func(t *testing.T) {
		builder := NewKongRoute().
			WithName("test-route").
			WithNamespace("test-namespace").
			WithHosts([]string{"example.com"})

		route, err := builder.Build()
		require.NoError(t, err)
		assert.Equal(t, "test-route", route.Name)
		assert.Equal(t, "test-namespace", route.Namespace)
		assert.Equal(t, []string{"example.com"}, route.Spec.Hosts)
	})

	t.Run("build with errors", func(t *testing.T) {
		builder := NewKongRoute().WithOwner(nil)

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "owner cannot be nil")
	})
}

func TestKongRouteBuilder_MustBuild(t *testing.T) {
	t.Run("successful must build", func(t *testing.T) {
		builder := NewKongRoute().WithName("test-route")

		route := builder.MustBuild()
		assert.Equal(t, "test-route", route.Name)
	})

	t.Run("must build with errors panics", func(t *testing.T) {
		builder := NewKongRoute().WithOwner(nil)

		assert.Panics(t, func() {
			builder.MustBuild()
		})
	})
}

func TestKongRouteBuilder_Chaining(t *testing.T) {
	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-http-route",
			Namespace: "test-namespace",
			UID:       "test-uid",
		},
	}

	pathType := gatewayv1.PathMatchPathPrefix
	pathValue := "/api"
	method := gatewayv1.HTTPMethodGet

	match := gwtypes.HTTPRouteMatch{
		Path: &gatewayv1.HTTPPathMatch{
			Type:  &pathType,
			Value: &pathValue,
		},
		Method: &method,
	}

	parentRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	route := NewKongRoute().
		WithName("test-route").
		WithNamespace("test-namespace").
		WithSpecName("test-spec").
		WithStripPath(true).
		WithHosts([]string{"example.com"}).
		WithHTTPRouteMatch(match, false).
		WithKongService("test-service").
		WithOwner(httpRoute).
		WithLabels(httpRoute, parentRef).
		WithAnnotations(httpRoute, parentRef).
		MustBuild()

	assert.Equal(t, "test-route", route.Name)
	assert.Equal(t, "test-namespace", route.Namespace)
	assert.Equal(t, "test-spec", *route.Spec.Name)
	assert.Equal(t, true, *route.Spec.StripPath)
	assert.Equal(t, []string{"example.com"}, route.Spec.Hosts)
	assert.Equal(t, []string{"~/api$", "/api/"}, route.Spec.Paths)
	assert.Equal(t, []string{"GET"}, route.Spec.Methods)
	assert.Equal(t, "test-service", route.Spec.ServiceRef.NamespacedRef.Name)
	assert.Len(t, route.OwnerReferences, 1)
	assert.NotNil(t, route.Labels)
	assert.NotNil(t, route.Annotations)
}

func TestKongRouteBuilder_MultipleErrors(t *testing.T) {
	builder := NewKongRoute()

	builder.WithOwner(nil)
	builder.errors = append(builder.errors, assert.AnError)

	_, err := builder.Build()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "owner cannot be nil")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}
