package kongroute

import (
	"context"
	"testing"

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
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestRouteForRule(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create a scheme with the necessary types
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	// Create test HTTPRoute
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

	// Create test rule
	rule := gwtypes.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{
			{
				Path: &gatewayv1.HTTPPathMatch{
					Type:  (*gatewayv1.PathMatchType)(new("PathPrefix")),
					Value: new("/test"),
				},
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

	tests := []struct {
		name          string
		existingRoute *configurationv1alpha1.KongRoute
		serviceName   string
		hostnames     []string
		expectError   bool
		expectUpdate  bool
	}{
		{
			name:        "create new route",
			serviceName: "test-service",
			hostnames:   []string{"example.com"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			if tt.existingRoute != nil {
				objects = append(objects, tt.existingRoute)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result, err := RouteForRule(ctx, logger, fakeClient, httpRoute, rule, pRef, cpRef, tt.serviceName, tt.hostnames)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			// Verify the route has correct properties
			assert.Equal(t, "test-namespace", result.Namespace)
			assert.NotEmpty(t, result.Name)
			assert.Equal(t, tt.hostnames, result.Spec.Hosts)

			// Verify service reference
			if tt.serviceName != "" {
				require.NotNil(t, result.Spec.ServiceRef)
				assert.Equal(t, configurationv1alpha1.ServiceRefNamespacedRef, result.Spec.ServiceRef.Type)
				require.NotNil(t, result.Spec.ServiceRef.NamespacedRef)
				assert.Equal(t, tt.serviceName, result.Spec.ServiceRef.NamespacedRef.Name)
			}

			// Verify HTTPRoute annotation
			expectedAnnotation := httpRoute.Namespace + "/" + httpRoute.Name
			assert.Contains(t, result.Annotations[consts.GatewayOperatorHybridRoutesAnnotation], expectedAnnotation)

			// Verify paths from rule matches
			if len(rule.Matches) > 0 && rule.Matches[0].Path != nil {
				paths, _ := builder.GenerateKongRoutePathFromHTTPRouteMatch(rule.Matches[0].Path, false)
				assert.Subset(t, result.Spec.Paths, paths)
			}
		})
	}
}
