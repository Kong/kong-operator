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
			{Path: &gatewayv1.HTTPPathMatch{Type: &[]gatewayv1.PathMatchType{gatewayv1.PathMatchPathPrefix}[0], Value: new("/test")}},
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
					{Path: &gatewayv1.HTTPPathMatch{Type: &[]gatewayv1.PathMatchType{gatewayv1.PathMatchPathPrefix}[0], Value: new("/test")}},
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
			expected: &[]int64{5000}[0],
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
			expected: &[]int64{1000}[0],
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
				assert.Nil(t, service.Spec.ConnectTimeout)
			} else {
				require.NotNil(t, service.Spec.ConnectTimeout)
				assert.Equal(t, *tt.expected, *service.Spec.ConnectTimeout)
			}
		})
	}
}

//go:fix inline
func int64Ptr(v int64) *int64 { return new(v) }

func TestResolveConnectTimeoutFromHTTPRouteBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	port80 := gatewayv1.PortNumber(80)

	tests := []struct {
		name            string
		backendRefs     []gatewayv1.HTTPBackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name: "service with connect-timeout annotation returns value",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-timeout", Port: &port80}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: int64Ptr(5000),
		},
		{
			name: "service without annotation returns nil",
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
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "1000"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "2000"}}},
			},
			expected: int64Ptr(1000),
		},
		{
			name:            "no backend refs returns nil",
			backendRefs:     []gatewayv1.HTTPBackendRef{},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name: "service does not exist returns nil",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc", Port: &port80}}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name: "unsupported backend ref returns nil",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Port:  &port80,
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
					Kind:  &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				}}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name: "cross-namespace backend ref",
			backendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other-ns",
					Port:      &port80,
					Namespace: &[]gatewayv1.Namespace{"other-namespace"}[0],
				}}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "3000"}}},
			},
			expected: int64Ptr(3000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpRoute := &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "test-namespace"},
			}
			rule := gwtypes.HTTPRouteRule{BackendRefs: tt.backendRefs}

			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := resolveConnectTimeoutFromHTTPRouteBackendRefs(ctx, cl, httpRoute, rule, logger)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, *tt.expected, *result)
			}
		})
	}
}

func TestResolveConnectTimeoutFromTLSRouteBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name            string
		backendRefs     []gwtypes.BackendRef
		backendServices []corev1.Service
		expected        *int64
	}{
		{
			name: "service with connect-timeout annotation returns value",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-timeout"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-with-timeout", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "5000"}}},
			},
			expected: int64Ptr(5000),
		},
		{
			name: "service without annotation returns nil",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-timeout"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-no-timeout", Namespace: "test-namespace"}},
			},
			expected: nil,
		},
		{
			name: "first backend ref with annotation wins",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-first"}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-second"}},
			},
			backendServices: []corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-first", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "1000"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-second", Namespace: "test-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "2000"}}},
			},
			expected: int64Ptr(1000),
		},
		{
			name:            "no backend refs returns nil",
			backendRefs:     []gwtypes.BackendRef{},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name: "service does not exist returns nil",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "nonexistent-svc"}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
		{
			name: "unsupported backend ref returns nil",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "some-ref",
					Group: &[]gatewayv1.Group{gatewayv1.Group("example.com")}[0],
					Kind:  &[]gatewayv1.Kind{gatewayv1.Kind("NotService")}[0],
				}},
			},
			backendServices: []corev1.Service{},
			expected:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsRoute := &gwtypes.TLSRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "test-tls-route", Namespace: "test-namespace"},
			}
			rule := gwtypes.TLSRouteRule{BackendRefs: tt.backendRefs}

			var objects []client.Object
			for i := range tt.backendServices {
				objects = append(objects, &tt.backendServices[i])
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			result := resolveConnectTimeoutFromTLSRouteBackendRefs(ctx, cl, tlsRoute, rule, logger)
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
			expected: int64Ptr(5000),
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
			expected: int64Ptr(0),
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
				{ObjectMeta: metav1.ObjectMeta{Name: "svc-other-ns", Namespace: "other-namespace", Annotations: map[string]string{"konghq.com/connect-timeout": "3000"}}},
			},
			expected: int64Ptr(3000),
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
