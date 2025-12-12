package upstream

import (
	"context"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

func TestAppendHTTPRouteToAnnotations(t *testing.T) {
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
			upstream := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-upstream",
					Namespace:   "test-namespace",
					Annotations: tt.existingAnnotations,
				},
			}

			am := metadata.NewAnnotationManager(logger)
			am.AppendRouteToAnnotation(upstream, tt.httpRoute)
			actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
			assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
		})
	}
}

func TestRemoveHTTPRouteFromAnnotations(t *testing.T) {
	logger := logr.Discard()

	tests := []struct {
		name                    string
		existingAnnotations     map[string]string
		httpRoute               *gwtypes.HTTPRoute
		expectedAnnotation      string
		expectModification      bool
		expectAnnotationDeleted bool
	}{
		{
			name:                "no annotations",
			existingAnnotations: nil,
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "",
			expectModification: false,
		},
		{
			name: "route not in annotation",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route",
			expectModification: false,
		},
		{
			name: "remove only route - annotation should be deleted",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation:      "",
			expectModification:      true,
			expectAnnotationDeleted: true,
		},
		{
			name: "remove first route from multiple",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "test-namespace/test-route,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns2/route2",
			expectModification: true,
		},
		{
			name: "remove middle route from multiple",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns1/route1,ns3/route3",
			expectModification: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &configurationv1alpha1.KongUpstream{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-upstream",
					Namespace:   "test-namespace",
					Annotations: make(map[string]string),
				},
			}

			// Copy existing annotations
			if tt.existingAnnotations != nil {
				maps.Copy(upstream.Annotations, tt.existingAnnotations)
			}

			am := metadata.NewAnnotationManager(logger)
			modified := am.RemoveRouteFromAnnotation(upstream, tt.httpRoute)
			assert.Equal(t, tt.expectModification, modified)

			if tt.expectAnnotationDeleted {
				_, exists := upstream.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
				assert.False(t, exists, "annotation should be deleted")
			} else if tt.expectedAnnotation != "" {
				actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
				assert.Equal(t, tt.expectedAnnotation, actualAnnotation)
			}
		})
	}
}

func TestUpstreamForRule_NewUpstream(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := logr.Discard()
	ctx := context.Background()

	httpRoute := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-namespace",
		},
	}

	rule := gwtypes.HTTPRouteRule{
		BackendRefs: []gwtypes.HTTPBackendRef{
			{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name: "test-service",
						Port: func() *gwtypes.PortNumber { p := gwtypes.PortNumber(80); return &p }(),
					},
				},
			},
		},
	}

	pRef := &gwtypes.ParentReference{
		Name: "test-gateway",
	}

	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-cp",
			Namespace: "test-namespace",
		},
	}

	upstream, _, err := UpstreamForRule(ctx, logger, client, httpRoute, rule, pRef, cp)
	require.NoError(t, err)
	require.NotNil(t, upstream)

	// Verify the upstream has the expected annotation
	expectedAnnotation := "test-namespace/test-route"
	actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	assert.Equal(t, expectedAnnotation, actualAnnotation)
}
