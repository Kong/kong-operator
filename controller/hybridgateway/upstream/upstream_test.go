package upstream

import (
	"context"
	"maps"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	httpRouteTypeMeta = metav1.TypeMeta{
		Kind:       "HTTPRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
	}
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
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
			actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
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
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route,ns2/route2",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
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
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,test-namespace/test-route,ns3/route3",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "ns1/route1,ns3/route3",
			expectModification: true,
		},
		{
			name: "same namespace and name but different kind",
			existingAnnotations: map[string]string{
				consts.GatewayOperatorHybridRoutesTLSRouteAnnotation:  "test-namespace/test-route,ns1/route1",
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
			},
			httpRoute: &gwtypes.HTTPRoute{
				TypeMeta: httpRouteTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "other-namespace/other-route",
			expectModification: false,
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
				_, exists := upstream.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
				assert.False(t, exists, "annotation should be deleted")
			} else if tt.expectedAnnotation != "" {
				actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
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
		TypeMeta: httpRouteTypeMeta,
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

	upstream, err := UpstreamForRule(ctx, logger, client, httpRoute, rule, pRef, cp)
	require.NoError(t, err)
	require.NotNil(t, upstream)

	// Verify the upstream has the expected annotation
	expectedAnnotation := "test-namespace/test-route"
	actualAnnotation := upstream.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
	assert.Equal(t, expectedAnnotation, actualAnnotation)
}

func TestUpstreamForRule_WithUpstreamPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1beta1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()
	logger := logr.Discard()

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
			},
		},
	}
	policy := &configurationv1beta1.KongUpstreamPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "test-namespace",
		},
		Spec: configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: func() *string { s := "least-connections"; return &s }(),
			Slots:     func() *int { v := 100; return &v }(),
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, policy).
		Build()

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

	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-cp",
			Namespace: "test-namespace",
		},
	}

	upstream, err := UpstreamForRule(ctx, logger, cl, httpRoute, rule, pRef, cp)
	require.NoError(t, err)
	require.NotNil(t, upstream)

	require.NotNil(t, upstream.Spec.Algorithm)
	assert.Equal(t, sdkkonnectcomp.UpstreamAlgorithm("least-connections"), *upstream.Spec.Algorithm)
	require.NotNil(t, upstream.Spec.Slots)
	assert.Equal(t, int64(100), *upstream.Spec.Slots)
}

func TestUpstreamForRule_InconsistentUpstreamPolicies(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1beta1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := context.Background()
	logger := logr.Discard()

	// Two services with different policies.
	svc1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-a",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				configurationv1beta1.KongUpstreamPolicyAnnotationKey: "policy-a",
			},
		},
	}
	svc2 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-b",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				configurationv1beta1.KongUpstreamPolicyAnnotationKey: "policy-b",
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc1, svc2).
		Build()

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
						Name: "svc-a",
						Port: func() *gwtypes.PortNumber { p := gwtypes.PortNumber(80); return &p }(),
					},
				},
			},
			{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name: "svc-b",
						Port: func() *gwtypes.PortNumber { p := gwtypes.PortNumber(80); return &p }(),
					},
				},
			},
		},
	}

	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      "test-cp",
			Namespace: "test-namespace",
		},
	}

	upstream, err := UpstreamForRule(ctx, logger, cl, httpRoute, rule, pRef, cp)
	require.NoError(t, err)
	require.NotNil(t, upstream)

	// Policy must not be applied when annotations are inconsistent.
	assert.Nil(t, upstream.Spec.Algorithm)
	assert.Nil(t, upstream.Spec.Slots)
}
