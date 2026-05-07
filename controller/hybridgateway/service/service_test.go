package service

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var (
	httpRouteTypeMeta = metav1.TypeMeta{
		Kind:       "HTTPRoute",
		APIVersion: "gateway.networking.k8s.io/v1",
	}
)

func TestServiceForRule(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	// Create a scheme with the necessary types
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

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

	// Create test rule
	rule := gwtypes.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{
			{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new("/test")}},
		},
	}

	// Create parent reference
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}

	// Create control plane reference
	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}

	upstreamName := "test-upstream"
	serviceName := namegen.NewKongServiceNameForHTTPRouteRule(httpRoute, cp, rule)

	tests := []struct {
		name               string
		existingService    *configurationv1alpha1.KongService
		expectedAnnotation string
		expectUpdate       bool
		expectedHost       string
	}{
		{
			name:               "new service creation",
			existingService:    nil,
			expectedAnnotation: "test-namespace/test-route",
			expectUpdate:       false,
			expectedHost:       upstreamName,
		},
		{
			name: "existing service with no annotation",
			existingService: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "test-namespace",
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectUpdate:       true,
			expectedHost:       upstreamName,
		},
		{
			name: "existing service with different route annotation",
			existingService: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "other-namespace/other-route",
					},
				},
			},
			expectedAnnotation: "other-namespace/other-route,test-namespace/test-route",
			expectUpdate:       true,
			expectedHost:       upstreamName,
		},
		{
			name: "existing service with same route annotation",
			existingService: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "test-namespace/test-route",
					},
				},
			},
			expectedAnnotation: "test-namespace/test-route",
			expectUpdate:       false,
			expectedHost:       upstreamName,
		},
		{
			name: "existing service with multiple routes",
			existingService: &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: "test-namespace",
					Annotations: map[string]string{
						consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns1/route1,ns2/route2",
					},
				},
			},
			expectedAnnotation: "ns1/route1,ns2/route2,test-namespace/test-route",
			expectUpdate:       true,
			expectedHost:       upstreamName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create client with or without existing service
			var objects []client.Object
			if tt.existingService != nil {
				objects = append(objects, tt.existingService)
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)

			assert.NoError(t, err)
			assert.NotNil(t, service)
			assert.NotEmpty(t, service.Name) // Name is generated by namegen
			assert.Equal(t, "test-namespace", service.Namespace)
			assert.Equal(t, tt.expectedHost, service.Spec.Host)

			// Check annotation
			annotations := service.GetAnnotations()
			assert.NotNil(t, annotations)
			assert.Equal(t, tt.expectedAnnotation, annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
		})
	}
}

func TestServiceForRule_ProtocolAnnotation(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	upstreamName := "test-upstream"
	port443 := gatewayv1.PortNumber(443)
	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name             string
		backendRefs      []gatewayv1.HTTPBackendRef
		backendServices  []corev1.Service
		expectedProtocol sdkkonnectcomp.Protocol
	}{
		{
			name: "backend service with https protocol annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "my-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/protocol": "https",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 443}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTPS,
		},
		{
			name: "backend service with grpcs protocol annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "grpc-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grpc-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/protocol": "grpcs",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 443}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolGrpcs,
		},
		{
			name: "backend service without protocol annotation defaults to http",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "plain-svc",
							Port: &port80,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "plain-svc",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTP,
		},
		{
			name: "backend service with invalid protocol annotation defaults to http",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "bad-svc",
							Port: &port80,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/protocol": "invalid-protocol",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTP,
		},
		{
			name: "multiple backend refs, first with annotation wins",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "svc-with-annotation",
							Port: &port443,
						},
					},
				},
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "svc-without-annotation",
							Port: &port80,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-with-annotation",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/protocol": "https",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 443}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-without-annotation",
						Namespace: "test-namespace",
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTPS,
		},
		{
			name: "backend service with upper case protocol annotation is normalized",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "upper-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "upper-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/protocol": "HTTPS",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 443}},
					},
				},
			},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTPS,
		},
		{
			name:             "no backend refs defaults to http",
			backendRefs:      []gatewayv1.HTTPBackendRef{},
			backendServices:  []corev1.Service{},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTP,
		},
		{
			name: "backend service does not exist defaults to http",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "nonexistent-svc",
							Port: &port80,
						},
					},
				},
			},
			backendServices:  []corev1.Service{},
			expectedProtocol: sdkkonnectcomp.ProtocolHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			rule := gwtypes.HTTPRouteRule{
				BackendRefs: tt.backendRefs,
				Matches: []gatewayv1.HTTPRouteMatch{
					{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new("/test")}},
				},
			}

			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)

			require.NoError(t, err)
			require.NotNil(t, service)
			assert.Equal(t, tt.expectedProtocol, service.Spec.Protocol)
		})
	}
}

func TestServiceForRule_PathAnnotation(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	upstreamName := "test-upstream"
	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		backendRefs     []gatewayv1.HTTPBackendRef
		backendServices []corev1.Service
		expected        *string
	}{
		{
			name: "service with path annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "my-svc", Port: &port80}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/api/v1"}}},
			},
			expected: new("/api/v1"),
		},
		{
			name: "service without annotation leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port80}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name: "first backend ref with annotation wins",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port80}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/first"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/second"}}},
			},
			expected: new("/first"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRoute := &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "test-namespace"},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "test-gateway"}}},
				},
			}
			rule := gwtypes.HTTPRouteRule{
				BackendRefs: tt.backendRefs,
				Matches: []gatewayv1.HTTPRouteMatch{
					{Path: &gatewayv1.HTTPPathMatch{Type: &[]gatewayv1.PathMatchType{gatewayv1.PathMatchPathPrefix}[0], Value: new("/test")}},
				},
			}
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)
			require.NoError(t, err)
			require.NotNil(t, service)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.Path)
			} else {
				require.NotNil(t, service.Spec.Path)
				assert.Equal(t, *tt.expected, *service.Spec.Path)
			}
		})
	}
}

func TestResolvePathFromBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		namespace       string
		backendRefs     []gwtypes.BackendRef
		backendServices []corev1.Service
		expectedPath    string
	}{
		{
			name:      "service with path annotation returns path",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-path", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-path", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/api/v1"}}},
			},
			expectedPath: "/api/v1",
		},
		{
			name:      "service without path annotation returns empty",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-path", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-path", Namespace: "test-namespace"}},
			},
			expectedPath: "",
		},
		{
			name:      "first backend ref with annotation wins",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-first", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-second", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-first", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/first"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-second", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/second"}}},
			},
			expectedPath: "/first",
		},
		{
			name:            "no backend refs returns empty",
			namespace:       "test-namespace",
			backendRefs:     []gwtypes.BackendRef{},
			backendServices: []corev1.Service{},
			expectedPath:    "",
		},
		{
			name:      "service does not exist returns empty",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{},
			expectedPath:    "",
		},
		{
			name:      "unsupported backend ref returns empty",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
					Kind:  &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				}},
			},
			backendServices: []corev1.Service{},
			expectedPath:    "",
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
				}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/path": "/other"}}},
			},
			expectedPath: "/other",
		},
		// TLSRoute-style backend refs (no port, same BackendRef type)
		{
			name:      "tls-style backend ref with path annotation returns path",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "tls-svc"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "tls-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/tls-path"}}},
			},
			expectedPath: "/tls-path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			path := resolvePathFromBackendRefs(ctx, cl, tt.namespace, tt.backendRefs, logger)
			assert.Equal(t, tt.expectedPath, path)
		})
	}
}

func TestExtractPathFromBackendRef(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name         string
		namespace    string
		backendRef   gwtypes.BackendRef
		services     []corev1.Service
		expectedPath string
		expectedOk   bool
	}{
		{
			name:      "supported backend ref with path annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-path", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-path", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": "/extracted"}}},
			},
			expectedPath: "/extracted",
			expectedOk:   true,
		},
		{
			name:      "supported backend ref without path annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-path", Port: &port80},
			},
			services:   []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-path", Namespace: "test-namespace"}}},
			expectedOk: false,
		},
		{
			name:      "unsupported backend ref group",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
				},
			},
			expectedOk: false,
		},
		{
			name:      "unsupported backend ref kind",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "some-ref",
					Port: &port80,
					Kind: &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				},
			},
			expectedOk: false,
		},
		{
			name:      "backend service does not exist",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80},
			},
			expectedOk: false,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
				},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/path": "/other-ns"}}},
			},
			expectedPath: "/other-ns",
			expectedOk:   true,
		},
		{
			name:      "empty path annotation value",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-empty-path", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-empty-path", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/path": ""}}},
			},
			expectedOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.services {
				objects = append(objects, &tt.services[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			path, ok := extractPathFromBackendRef(ctx, cl, logger, tt.namespace, tt.backendRef)
			assert.Equal(t, tt.expectedOk, ok)
			if tt.expectedOk {
				assert.Equal(t, tt.expectedPath, path)
			} else {
				assert.Empty(t, path)
			}
		})
	}
}

func TestServiceForRule_TLSVerifyAnnotation(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	upstreamName := "test-upstream"
	port443 := gatewayv1.PortNumber(443)

	tests := []struct {
		name            string
		backendRefs     []gatewayv1.HTTPBackendRef
		backendServices []corev1.Service
		expected        *bool
	}{
		{
			name:        "service with tls-verify=true annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "my-svc", Port: &port443}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
		{
			name:        "service with tls-verify=false annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "my-svc", Port: &port443}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "false"}}},
			},
			expected: &[]bool{false}[0],
		},
		{
			name:        "service without annotation leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port443}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:        "invalid value leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "bad-svc", Port: &port443}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "bad-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "maybe"}}},
			},
			expected: nil,
		},
		{
			name: "first backend ref with annotation wins",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port443}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port443}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "false"}}},
			},
			expected: new(true),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRoute := &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "test-namespace"},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "test-gateway"}}},
				},
			}
			rule := gwtypes.HTTPRouteRule{
				BackendRefs: tt.backendRefs,
				Matches: []gatewayv1.HTTPRouteMatch{
					{Path: &gatewayv1.HTTPPathMatch{Type: &[]gatewayv1.PathMatchType{gatewayv1.PathMatchPathPrefix}[0], Value: new("/test")}},
				},
			}
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)
			require.NoError(t, err)
			require.NotNil(t, service)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.TLSVerify)
			} else {
				require.NotNil(t, service.Spec.TLSVerify)
				assert.Equal(t, *tt.expected, *service.Spec.TLSVerify)
			}
		})
	}
}

func TestResolveTLSVerifyFromBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		namespace       string
		backendRefs     []gwtypes.BackendRef
		backendServices []corev1.Service
		expected        *bool
	}{
		{
			name:      "service with tls-verify=true annotation returns true",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-verify-true", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-verify-true", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
		{
			name:      "service with tls-verify=false annotation returns false",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-verify-false", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-verify-false", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "false"}}},
			},
			expected: new(false),
		},
		{
			name:      "service without annotation returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "first backend ref with annotation wins",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "false"}}},
			},
			expected: new(true),
		},
		{
			name:            "no backend refs returns nil",
			namespace:       "test-namespace",
			backendRefs:     []gwtypes.BackendRef{},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "service does not exist returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "unsupported backend ref returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
					Kind:  &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
				}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
		// TLSRoute-style backend refs (no port, same BackendRef type)
		{
			name:      "tls-style backend ref with tls-verify=true returns true",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "tls-svc"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "tls-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := resolveTLSVerifyFromBackendRefs(ctx, cl, tt.namespace, tt.backendRefs, logger)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestHTTPBackendRefsToBackendRefs(t *testing.T) {
	port80 := gatewayv1.PortNumber(80)
	port443 := gatewayv1.PortNumber(443)
	weight := int32(50)
	otherNS := gatewayv1.Namespace("other-namespace")

	tests := []struct {
		name     string
		input    []gatewayv1.HTTPBackendRef
		expected []gwtypes.BackendRef
	}{
		{
			name:     "nil input returns empty slice",
			input:    nil,
			expected: []gwtypes.BackendRef{},
		},
		{
			name:     "empty input returns empty slice",
			input:    []gatewayv1.HTTPBackendRef{},
			expected: []gwtypes.BackendRef{},
		},
		{
			name: "single ref extracted",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
			},
		},
		{
			name: "multiple refs extracted in order",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port443}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port443}},
			},
		},
		{
			name: "HTTP filters are stripped, only BackendRef preserved",
			input: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-filtered", Port: &port80}},
					Filters: []gatewayv1.HTTPRouteFilter{
						{Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier},
					},
				},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-filtered", Port: &port80}},
			},
		},
		{
			name: "cross-namespace ref preserved",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other",
					Port:      &port80,
					Namespace: &otherNS,
				}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other",
					Port:      &port80,
					Namespace: &otherNS,
				}},
			},
		},
		{
			name: "weight preserved",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-weighted", Port: &port80},
					Weight:                 &weight,
				}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-weighted", Port: &port80}, Weight: &weight},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := httpBackendRefsToBackendRefs(tt.input)
			require.Len(t, got, len(tt.expected))
			for i := range tt.expected {
				assert.Equal(t, tt.expected[i].Name, got[i].Name)
				assert.Equal(t, tt.expected[i].Namespace, got[i].Namespace)
				assert.Equal(t, tt.expected[i].Port, got[i].Port)
				assert.Equal(t, tt.expected[i].Weight, got[i].Weight)
			}
		})
	}
}

func TestExtractTLSVerifyFromBackendRef(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name       string
		namespace  string
		backendRef gwtypes.BackendRef
		services   []corev1.Service
		expected   *bool
	}{
		{
			name:      "supported backend ref with tls-verify=true annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-verify-true", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-verify-true", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
		{
			name:      "supported backend ref with tls-verify=false annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-verify-false", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-verify-false", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "false"}}},
			},
			expected: new(false),
		},
		{
			name:      "supported backend ref without annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-verify", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-verify", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "invalid annotation value returns nil",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-bad-verify", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-bad-verify", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "maybe"}}},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref group",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
				},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref kind",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "some-ref",
					Port: &port80,
					Kind: &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				},
			},
			expected: nil,
		},
		{
			name:      "backend service does not exist",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80},
			},
			expected: nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
				},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/tls-verify": "true"}}},
			},
			expected: new(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.services {
				objects = append(objects, &tt.services[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := extractTLSVerifyFromBackendRef(ctx, cl, logger, tt.namespace, tt.backendRef)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestServiceForRule_TLSVerifyDepthAnnotation(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name: "test-cp",
		},
	}
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	upstreamName := "test-upstream"
	port443 := gatewayv1.PortNumber(443)

	tests := []struct {
		name            string
		backendRefs     []gatewayv1.HTTPBackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name: "backend service with tls-verify-depth annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "my-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/tls-verify-depth": "3",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 443}},
					},
				},
			},
			expected: new(int64(3)),
		},
		{
			name: "backend service without annotation leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "plain-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "plain-svc",
						Namespace: "test-namespace",
					},
				},
			},
			expected: nil,
		},
		{
			name: "invalid annotation value leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "bad-svc",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-svc",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/tls-verify-depth": "abc",
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "first backend ref with annotation wins",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "svc-with-annotation",
							Port: &port443,
						},
					},
				},
				{
					BackendRef: gatewayv1.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "svc-without-annotation",
							Port: &port443,
						},
					},
				},
			},
			backendServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-with-annotation",
						Namespace: "test-namespace",
						Annotations: map[string]string{
							"konghq.com/tls-verify-depth": "5",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-without-annotation",
						Namespace: "test-namespace",
					},
				},
			},
			expected: new(int64(5)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			rule := gwtypes.HTTPRouteRule{
				BackendRefs: tt.backendRefs,
				Matches: []gatewayv1.HTTPRouteMatch{
					{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new("/test")}},
				},
			}

			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)

			require.NoError(t, err)
			require.NotNil(t, service)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.TLSVerifyDepth)
			} else {
				require.NotNil(t, service.Spec.TLSVerifyDepth)
				assert.Equal(t, *tt.expected, *service.Spec.TLSVerifyDepth)
			}
		})
	}
}

func TestResolveTLSVerifyDepthFromBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		namespace       string
		backendRefs     []gwtypes.BackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name:      "service with tls-verify-depth annotation returns value",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-depth", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-depth", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "3"}}},
			},
			expected: new(int64(3)),
		},
		{
			name:      "service without annotation returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "first backend ref with annotation wins",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "2"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "5"}}},
			},
			expected: new(int64(2)),
		},
		{
			name:            "no backend refs returns nil",
			namespace:       "test-namespace",
			backendRefs:     []gwtypes.BackendRef{},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "service does not exist returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "unsupported backend ref returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: new(gatewayv1.Group("example.com")),
					Kind:  new(gatewayv1.Kind("NotService")),
				}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: new(gatewayv1.Namespace("other-namespace")),
				}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "4"}}},
			},
			expected: new(int64(4)),
		},
		// TLS-style backend refs (no port, same BackendRef type)
		{
			name:      "tls-style backend ref with annotation returns value",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "tls-svc"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "tls-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "3"}}},
			},
			expected: new(int64(3)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := resolveTLSVerifyDepthFromBackendRefs(ctx, cl, tt.namespace, tt.backendRefs, logger)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestExtractTLSVerifyDepthFromBackendRef(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name       string
		namespace  string
		backendRef gwtypes.BackendRef
		services   []corev1.Service
		expected   *int64
	}{
		{
			name:      "supported backend ref with tls-verify-depth annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-depth", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-depth", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "3"}}},
			},
			expected: new(int64(3)),
		},
		{
			name:      "supported backend ref without annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-depth", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-depth", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "zero value is valid",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-zero-depth", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-zero-depth", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "0"}}},
			},
			expected: new(int64(0)),
		},
		{
			name:      "negative value returns nil",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-neg-depth", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-neg-depth", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "-1"}}},
			},
			expected: nil,
		},
		{
			name:      "non-numeric value returns nil",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-bad-depth", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-bad-depth", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "abc"}}},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref group",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: new(gatewayv1.Group("example.com")),
				},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref kind",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "some-ref",
					Port: &port80,
					Kind: new(gatewayv1.Kind("NotService")),
				},
			},
			expected: nil,
		},
		{
			name:      "backend service does not exist",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80},
			},
			expected: nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: new(gatewayv1.Namespace("other-namespace")),
				},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/tls-verify-depth": "4"}}},
			},
			expected: new(int64(4)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.services {
				objects = append(objects, &tt.services[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := extractTLSVerifyDepthFromBackendRef(ctx, cl, logger, tt.namespace, tt.backendRef)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestServiceForRule_ConnectTimeoutAnnotation(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}
	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	upstreamName := "test-upstream"
	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		backendRefs     []gatewayv1.HTTPBackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name:        "service with connect-timeout annotation",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "my-svc", Port: &port80}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: new(int64(5000)),
		},
		{
			name:        "service without annotation leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port80}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:        "invalid value leaves field unset",
			backendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "bad-svc", Port: &port80}}}},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "bad-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "abc"}}},
			},
			expected: nil,
		},
		{
			name: "first backend ref with annotation wins",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port80}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "1000"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "2000"}}},
			},
			expected: new(int64(1000)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRoute := &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "test-namespace"},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "test-gateway"}}},
				},
			}
			rule := gwtypes.HTTPRouteRule{
				BackendRefs: tt.backendRefs,
				Matches: []gatewayv1.HTTPRouteMatch{
					{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new("/test")}},
				},
			}
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			service, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, upstreamName)
			require.NoError(t, err)
			require.NotNil(t, service)
			if tt.expected == nil {
				assert.Nil(t, service.Spec.ConnectTimeout)
			} else {
				require.NotNil(t, service.Spec.ConnectTimeout)
				assert.Equal(t, *tt.expected, *service.Spec.ConnectTimeout)
			}
		})
	}
}

func TestResolveConnectTimeoutFromBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		namespace       string
		backendRefs     []gwtypes.BackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name:      "service with connect-timeout annotation returns value",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-timeout", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: new(int64(5000)),
		},
		{
			name:      "service without annotation returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "plain-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "plain-svc", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "first backend ref with annotation wins",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port80}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "1000"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "2000"}}},
			},
			expected: new(int64(1000)),
		},
		{
			name:            "no backend refs returns nil",
			namespace:       "test-namespace",
			backendRefs:     []gwtypes.BackendRef{},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "service does not exist returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "unsupported backend ref returns nil",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: new(gatewayv1.Group("example.com")),
					Kind:  new(gatewayv1.Kind("NotService")),
				}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: new(gatewayv1.Namespace("other-namespace")),
				}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "3000"}}},
			},
			expected: new(int64(3000)),
		},
		// TLS-style backend refs (no port, same BackendRef type)
		{
			name:      "tls-style backend ref with annotation returns value",
			namespace: "test-namespace",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "tls-svc"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "tls-svc", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: new(int64(5000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := resolveConnectTimeoutFromBackendRefs(ctx, cl, tt.namespace, tt.backendRefs, logger)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestExtractConnectTimeoutFromBackendRef(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name       string
		namespace  string
		backendRef gwtypes.BackendRef
		services   []corev1.Service
		expected   *int64
	}{
		{
			name:      "supported backend ref with connect-timeout annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-timeout", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: new(int64(5000)),
		},
		{
			name:      "supported backend ref without annotation",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-timeout", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-timeout", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name:      "zero value is valid",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-zero-timeout", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-zero-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "0"}}},
			},
			expected: new(int64(0)),
		},
		{
			name:      "negative value returns nil",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-neg-timeout", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-neg-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "-1"}}},
			},
			expected: nil,
		},
		{
			name:      "non-numeric value returns nil",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-bad-timeout", Port: &port80},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-bad-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "abc"}}},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref group",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: new(gatewayv1.Group("example.com")),
				},
			},
			expected: nil,
		},
		{
			name:      "unsupported backend ref kind",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "some-ref",
					Port: &port80,
					Kind: new(gatewayv1.Kind("NotService")),
				},
			},
			expected: nil,
		},
		{
			name:      "backend service does not exist",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80},
			},
			expected: nil,
		},
		{
			name:      "cross-namespace backend ref",
			namespace: "test-namespace",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: new(gatewayv1.Namespace("other-namespace")),
				},
			},
			services: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "3000"}}},
			},
			expected: new(int64(3000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for i := range tt.services {
				objects = append(objects, &tt.services[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := extractConnectTimeoutFromBackendRef(ctx, cl, logger, tt.namespace, tt.backendRef)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}
