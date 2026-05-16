package service

import (
	"context"
	"testing"

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

func TestIsNonTLSProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		want     bool
	}{
		{"http", true},
		{"grpc", true},
		{"tcp", true},
		{"tls_passthrough", true},
		{"udp", true},
		{"ws", true},
		{"https", false},
		{"grpcs", false},
		{"tls", false},
		{"wss", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			assert.Equal(t, tt.want, isNonTLSProtocol(tt.protocol))
		})
	}
}

func TestExtractClientCertFromBackendRef(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	svcWithAnnotation := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-with-cert",
			Namespace: "test-ns",
			Annotations: map[string]string{
				"konghq.com/client-cert": "my-secret",
			},
		},
	}
	svcWithoutAnnotation := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-no-cert",
			Namespace: "test-ns",
		},
	}

	tests := []struct {
		name           string
		backendRef     gwtypes.BackendRef
		objects        []client.Object
		wantSecretName string
		wantOk         bool
	}{
		{
			name: "backend service has annotation",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "svc-with-cert",
				},
			},
			objects:        []client.Object{&svcWithAnnotation},
			wantSecretName: "my-secret",
			wantOk:         true,
		},
		{
			name: "backend service lacks annotation",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "svc-no-cert",
				},
			},
			objects: []client.Object{&svcWithoutAnnotation},
			wantOk:  false,
		},
		{
			name: "backend service not found",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: "missing-svc",
				},
			},
			objects: []client.Object{},
			wantOk:  false,
		},
		{
			name: "unsupported group is ignored",
			backendRef: gwtypes.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:  "svc-with-cert",
					Group: func() *gatewayv1.Group { g := gatewayv1.Group("unknown.io"); return &g }(),
				},
			},
			objects: []client.Object{&svcWithAnnotation},
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			secretName, svc, ok := extractClientCertFromBackendRef(ctx, cl, logger, "test-ns", tt.backendRef)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantSecretName, secretName)
				require.NotNil(t, svc)
			} else {
				assert.Empty(t, secretName)
				assert.Nil(t, svc)
			}
		})
	}
}

func TestResolveClientCertFromBackendRefs(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name           string
		backendRefs    []gwtypes.BackendRef
		objects        []client.Object
		wantSecretName string
		wantSvcName    string
	}{
		{
			name:           "no backendRefs",
			backendRefs:    nil,
			objects:        []client.Object{},
			wantSecretName: "",
		},
		{
			name: "first backend has annotation",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a"}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b"}},
			},
			objects: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-a",
						Namespace: "ns",
						Annotations: map[string]string{
							"konghq.com/client-cert": "secret-a",
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-b",
						Namespace: "ns",
						Annotations: map[string]string{
							"konghq.com/client-cert": "secret-b",
						},
					},
				},
			},
			wantSecretName: "secret-a",
			wantSvcName:    "svc-a",
		},
		{
			name: "only second backend has annotation - first-wins from second",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-no-cert"}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-with-cert"}},
			},
			objects: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-no-cert", Namespace: "ns"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-with-cert",
						Namespace: "ns",
						Annotations: map[string]string{
							"konghq.com/client-cert": "my-secret",
						},
					},
				},
			},
			wantSecretName: "my-secret",
			wantSvcName:    "svc-with-cert",
		},
		{
			name: "no backend has annotation",
			backendRefs: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a"}},
			},
			objects: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "ns"},
				},
			},
			wantSecretName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			secretName, svc := resolveClientCertFromBackendRefs(ctx, cl, "ns", tt.backendRefs, logger)
			assert.Equal(t, tt.wantSecretName, secretName)
			if tt.wantSvcName != "" {
				require.NotNil(t, svc)
				assert.Equal(t, tt.wantSvcName, svc.Name)
			} else {
				assert.Nil(t, svc)
			}
		})
	}
}

func TestServiceForRule_ClientCertAnnotation(t *testing.T) {
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
	port443 := gatewayv1.PortNumber(443)

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "test-ns"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{Name: "test-gateway"}},
			},
		},
	}
	rule := gwtypes.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "my-svc",
						Port: &port443,
					},
				},
			},
		},
	}
	serviceName := namegen.NewKongServiceNameForHTTPRouteRule(httpRoute, cp, rule)

	certSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-client-cert", Namespace: "test-ns"},
		Data: map[string][]byte{
			"tls.crt": []byte("cert-data"),
			"tls.key": []byte("key-data"),
		},
	}

	tests := []struct {
		name                  string
		svcAnnotations        map[string]string
		secrets               []client.Object
		wantCertNotNil        bool
		wantCertName          string
		wantClientCertRef     bool
		wantServiceCertRefNil bool
	}{
		{
			name:                  "no annotation - no cert",
			svcAnnotations:        map[string]string{"konghq.com/protocol": "https"},
			secrets:               nil,
			wantCertNotNil:        false,
			wantServiceCertRefNil: true,
		},
		{
			name: "annotation + secret exists + tls protocol - cert built",
			svcAnnotations: map[string]string{
				"konghq.com/client-cert": "my-client-cert",
				"konghq.com/protocol":    "https",
			},
			secrets:           []client.Object{&certSecret},
			wantCertNotNil:    true,
			wantCertName:      serviceName,
			wantClientCertRef: true,
		},
		{
			name: "annotation + secret missing - no cert, service produced without ref",
			svcAnnotations: map[string]string{
				"konghq.com/client-cert": "missing-secret",
				"konghq.com/protocol":    "https",
			},
			secrets:               nil,
			wantCertNotNil:        false,
			wantServiceCertRefNil: true,
		},
		{
			name: "annotation + non-TLS protocol - no cert",
			svcAnnotations: map[string]string{
				"konghq.com/client-cert": "my-client-cert",
				"konghq.com/protocol":    "http",
			},
			secrets:               []client.Object{&certSecret},
			wantCertNotNil:        false,
			wantServiceCertRefNil: true,
		},
		{
			name: "annotation + default protocol (http) - no cert",
			svcAnnotations: map[string]string{
				"konghq.com/client-cert": "my-client-cert",
			},
			secrets:               []client.Object{&certSecret},
			wantCertNotNil:        false,
			wantServiceCertRefNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backendSvc := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "my-svc",
					Namespace:   "test-ns",
					Annotations: tt.svcAnnotations,
				},
			}
			objects := []client.Object{&backendSvc}
			objects = append(objects, tt.secrets...)

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			svc, cert, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, "test-upstream")
			require.NoError(t, err)
			require.NotNil(t, svc)

			if tt.wantCertNotNil {
				require.NotNil(t, cert, "expected KongCertificate to be non-nil")
				assert.Equal(t, tt.wantCertName, cert.Name)
				assert.Equal(t, "test-ns", cert.Namespace)
				require.NotNil(t, cert.Spec.SecretRef)
				assert.Equal(t, "my-client-cert", cert.Spec.SecretRef.Name)
			} else {
				assert.Nil(t, cert, "expected KongCertificate to be nil")
			}

			if tt.wantClientCertRef {
				require.NotNil(t, svc.Spec.ClientCertificateRef)
				assert.Equal(t, serviceName, svc.Spec.ClientCertificateRef.Name)
			}
			if tt.wantServiceCertRefNil {
				assert.Nil(t, svc.Spec.ClientCertificateRef)
				// Also verify the annotation is set correctly
				assert.Equal(t, "test-ns/test-route", svc.Annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
			}
		})
	}
}
