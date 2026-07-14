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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-client-cert",
			Namespace: "test-ns",
			Annotations: map[string]string{
				"konghq.com/tags": "cc-tag",
			},
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert-data"),
			"tls.key": []byte("key-data"),
		},
	}

	// certSecretNoTags carries no konghq.com/tags annotation; used to prove that tags on
	// the backend Service do not leak into the client-cert KongCertificate (Secret-only).
	certSecretNoTags := corev1.Secret{
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
		wantTags              commonv1alpha1.Tags
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
			wantTags:          commonv1alpha1.Tags{"cc-tag"},
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
		{
			// Secret-only invariant: tags on the backend Service must not leak into the
			// client-cert KongCertificate when the Secret itself carries no tags.
			name: "tags on backend Service do not leak into client-cert KongCertificate (Secret-only)",
			svcAnnotations: map[string]string{
				"konghq.com/client-cert": "my-client-cert",
				"konghq.com/protocol":    "https",
				"konghq.com/tags":        "svc-tag",
			},
			secrets:           []client.Object{&certSecretNoTags},
			wantCertNotNil:    true,
			wantCertName:      serviceName,
			wantClientCertRef: true,
			wantTags:          nil,
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

			svc, cert, _, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, "test-upstream")
			require.NoError(t, err)
			require.NotNil(t, svc)

			if tt.wantCertNotNil {
				require.NotNil(t, cert, "expected KongCertificate to be non-nil")
				assert.Equal(t, tt.wantCertName, cert.Name)
				assert.Equal(t, "test-ns", cert.Namespace)
				require.NotNil(t, cert.Spec.SecretRef)
				assert.Equal(t, "my-client-cert", cert.Spec.SecretRef.Name)
				assert.Equal(t, tt.wantTags, cert.Spec.Tags, "client-cert KongCertificate tags must come solely from the Secret")
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

// TestBuildClientCertReferenceGrant verifies that buildClientCertReferenceGrant returns nil
// for same-namespace references and a correctly populated KongReferenceGrant for cross-namespace ones.
func TestBuildClientCertReferenceGrant(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	pRef := &gwtypes.ParentReference{Name: "test-gateway"}
	route := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "route-ns"},
	}

	tests := []struct {
		name            string
		certNamespace   string
		secretNamespace string
		wantNil         bool
		wantFromNS      string
		wantSecretName  string
	}{
		{
			name:            "same namespace — no grant needed",
			certNamespace:   "gateway-ns",
			secretNamespace: "gateway-ns",
			wantNil:         true,
		},
		{
			name:            "cross-namespace — grant required",
			certNamespace:   "gateway-ns",
			secretNamespace: "service-ns",
			wantNil:         false,
			wantFromNS:      "gateway-ns",
			wantSecretName:  "my-cert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()

			grant := buildClientCertReferenceGrant(
				ctx, logger, cl, route, pRef,
				"grant-name", tt.certNamespace, "my-cert", tt.secretNamespace,
			)

			if tt.wantNil {
				assert.Nil(t, grant)
				return
			}

			require.NotNil(t, grant)
			assert.Equal(t, "grant-name", grant.Name)
			assert.Equal(t, tt.secretNamespace, grant.Namespace)

			require.Len(t, grant.Spec.From, 1)
			assert.Equal(t, configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), grant.Spec.From[0].Group)
			assert.Equal(t, configurationv1alpha1.Kind("KongCertificate"), grant.Spec.From[0].Kind)
			assert.Equal(t, configurationv1alpha1.Namespace(tt.wantFromNS), grant.Spec.From[0].Namespace)

			require.Len(t, grant.Spec.To, 1)
			assert.Equal(t, configurationv1alpha1.Group("core"), grant.Spec.To[0].Group)
			assert.Equal(t, configurationv1alpha1.Kind("Secret"), grant.Spec.To[0].Kind)
			require.NotNil(t, grant.Spec.To[0].Name)
			assert.Equal(t, configurationv1alpha1.ObjectName(tt.wantSecretName), *grant.Spec.To[0].Name)

			// Labels must be present so orphan cleanup can find the grant.
			assert.NotEmpty(t, grant.Labels)
		})
	}
}

// TestServiceForRule_ClientCertAnnotation_CrossNamespace verifies that when the KongCertificate
// and the Secret live in different namespaces, ServiceForRule also returns a KongReferenceGrant.
func TestServiceForRule_ClientCertAnnotation_CrossNamespace(t *testing.T) {
	ctx := context.Background()
	logger := zap.New()

	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cp := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "test-cp"},
	}

	// Gateway lives in "gateway-ns", route in "route-ns".
	gatewayNS := gatewayv1.Namespace("gateway-ns")
	pRef := &gwtypes.ParentReference{
		Name:      "test-gateway",
		Namespace: &gatewayNS,
	}
	port443 := gatewayv1.PortNumber(443)

	httpRoute := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "route-ns"},
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

	backendSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-svc",
			Namespace: "route-ns",
			Annotations: map[string]string{
				"konghq.com/client-cert": "my-client-cert",
				"konghq.com/protocol":    "https",
			},
		},
	}
	certSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-client-cert", Namespace: "route-ns"},
		Data:       map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(backendSvc, certSecret).Build()

	svc, cert, grant, err := ServiceForRule(ctx, logger, cl, httpRoute, rule, pRef, cp, "test-upstream")
	require.NoError(t, err)
	require.NotNil(t, svc)

	// KongCertificate must be in the Gateway namespace.
	require.NotNil(t, cert)
	assert.Equal(t, serviceName, cert.Name)
	assert.Equal(t, "gateway-ns", cert.Namespace)

	// KongReferenceGrant must be in the Secret's namespace (route-ns).
	require.NotNil(t, grant, "expected KongReferenceGrant for cross-namespace reference")
	assert.Equal(t, serviceName, grant.Name)
	assert.Equal(t, "route-ns", grant.Namespace)

	require.Len(t, grant.Spec.From, 1)
	assert.Equal(t, configurationv1alpha1.Namespace("gateway-ns"), grant.Spec.From[0].Namespace)
	assert.Equal(t, configurationv1alpha1.Kind("KongCertificate"), grant.Spec.From[0].Kind)

	require.Len(t, grant.Spec.To, 1)
	assert.Equal(t, configurationv1alpha1.Kind("Secret"), grant.Spec.To[0].Kind)
	require.NotNil(t, grant.Spec.To[0].Name)
	assert.Equal(t, configurationv1alpha1.ObjectName("my-client-cert"), *grant.Spec.To[0].Name)
}
