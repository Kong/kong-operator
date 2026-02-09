package converter

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/helpers/certificate"
)

func TestNewGatewayConverter(t *testing.T) {
	tests := []struct {
		name    string
		gateway *gwtypes.Gateway
	}{
		{
			name: "creates converter with empty gateway",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
		},
		{
			name: "creates converter with gateway with listeners",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway-with-listeners",
					Namespace: "test-ns",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							Port: 443,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			converter := newGatewayConverter(tt.gateway, fakeClient)

			require.NotNil(t, converter)

			// Verify internal state through GetRootObject
			rootObj := converter.GetRootObject()
			require.Equal(t, tt.gateway.Name, rootObj.Name)
			require.Equal(t, tt.gateway.Namespace, rootObj.Namespace)

			// Verify expected GVKs
			expectedGVKs := converter.GetExpectedGVKs()
			require.Len(t, expectedGVKs, 2)
			require.Contains(t, expectedGVKs, schema.GroupVersionKind{
				Group:   configurationv1alpha1.GroupVersion.Group,
				Version: configurationv1alpha1.GroupVersion.Version,
				Kind:    "KongCertificate",
			})
			require.Contains(t, expectedGVKs, schema.GroupVersionKind{
				Group:   configurationv1alpha1.GroupVersion.Group,
				Version: configurationv1alpha1.GroupVersion.Version,
				Kind:    "KongSNI",
			})
		})
	}
}

func TestBuildKongCertificate(t *testing.T) {
	tests := []struct {
		name            string
		gateway         *gwtypes.Gateway
		listener        *gwtypes.Listener
		certRef         gatewayv1.SecretObjectReference
		secretNamespace string
		controlPlaneRef *commonv1alpha1.ControlPlaneRef
		expectError     bool
	}{
		{
			name: "builds certificate with basic listener",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "tls-secret",
			},
			secretNamespace: "default",
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
		},
		{
			name: "builds certificate with cross-namespace secret",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway-cross-ns",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "tls-listener",
				Port: 8443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "secret-in-other-ns",
			},
			secretNamespace: "cert-manager",
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "cross-ns-cp",
				},
			},
			expectError: false,
		},
		{
			name: "builds certificate with high port number",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "high-port-gateway",
					Namespace: "test-ns",
				},
			},
			listener: &gwtypes.Listener{
				Name: "custom-port",
				Port: 65535,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "my-tls-cert",
			},
			secretNamespace: "test-ns",
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "high-port-cp",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			converter := newGatewayConverter(tt.gateway, fakeClient).(*gatewayConverter)
			converter.controlPlaneRef = tt.controlPlaneRef

			cert, err := converter.buildKongCertificate(tt.listener, tt.certRef, tt.secretNamespace)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, cert.Name)
			require.Equal(t, tt.gateway.Namespace, cert.Namespace)
			require.Equal(t, string(tt.certRef.Name), cert.Spec.SecretRef.Name)
			require.Equal(t, tt.secretNamespace, *cert.Spec.SecretRef.Namespace)
			require.Equal(t, tt.controlPlaneRef.KonnectNamespacedRef.Name, cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name)

			// Verify owner reference.
			require.Len(t, cert.OwnerReferences, 1)
			require.Equal(t, tt.gateway.Name, cert.OwnerReferences[0].Name)
		})
	}
}

func TestBuildKongSNI(t *testing.T) {
	tests := []struct {
		name        string
		gateway     *gwtypes.Gateway
		listener    *gwtypes.Listener
		kongCert    *configurationv1alpha1.KongCertificate
		expectError bool
		expectedSNI string
	}{
		{
			name: "builds SNI with explicit hostname",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name:     "https",
				Port:     443,
				Hostname: ptrTo(gatewayv1.Hostname("example.com")),
			},
			kongCert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-test-gateway-443",
					Namespace: "default",
				},
			},
			expectError: false,
			expectedSNI: "example.com",
		},
		{
			name: "builds SNI with wildcard when no hostname",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wildcard-gateway",
					Namespace: "test-ns",
				},
			},
			listener: &gwtypes.Listener{
				Name: "tls",
				Port: 8443,
			},
			kongCert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-wildcard-gateway-8443",
					Namespace: "test-ns",
				},
			},
			expectError: false,
			expectedSNI: "*",
		},
		{
			name: "builds SNI with wildcard when empty hostname",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-hostname-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name:     "listener",
				Port:     443,
				Hostname: ptrTo(gatewayv1.Hostname("")),
			},
			kongCert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-empty-hostname-gateway-443",
					Namespace: "default",
				},
			},
			expectError: false,
			expectedSNI: "*",
		},
		{
			name: "builds SNI with wildcard hostname",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wildcard-domain-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name:     "wildcard-listener",
				Port:     443,
				Hostname: ptrTo(gatewayv1.Hostname("*.example.com")),
			},
			kongCert: &configurationv1alpha1.KongCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-wildcard-domain-gateway-443",
					Namespace: "default",
				},
			},
			expectError: false,
			expectedSNI: "*.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			converter := newGatewayConverter(tt.gateway, fakeClient).(*gatewayConverter)

			sni, err := converter.buildKongSNI(tt.listener, tt.kongCert)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.kongCert.Name, sni.Name)
			require.Equal(t, tt.gateway.Namespace, sni.Namespace)
			require.Equal(t, tt.expectedSNI, sni.Spec.Name)
			require.Equal(t, tt.kongCert.Name, sni.Spec.CertificateRef.Name)

			// Verify owner reference.
			require.Len(t, sni.OwnerReferences, 1)
			require.Equal(t, tt.gateway.Name, sni.OwnerReferences[0].Name)
		})
	}
}

func TestProcessListenerCertificate(t *testing.T) {
	cert, key := certificate.MustGenerateCertPEMFormat()

	tests := []struct {
		name            string
		gateway         *gwtypes.Gateway
		listener        *gwtypes.Listener
		certRef         gatewayv1.SecretObjectReference
		setupMocks      func(*testing.T, client.Client)
		interceptorFunc *interceptor.Funcs
		controlPlaneRef *commonv1alpha1.ControlPlaneRef
		expectError     bool
		validateOutput  func(*testing.T, []client.Object)
	}{
		{
			name: "successfully processes valid TLS certificate",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name:     "https",
				Port:     443,
				Hostname: ptrTo(gatewayv1.Hostname("example.com")),
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "tls-secret",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create a valid TLS secret with proper PEM-encoded data.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": cert,
						"tls.key": key,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2)
				// Should have one KongCertificate and one KongSNI.
				var hasCert, hasSNI bool
				for _, obj := range objects {
					switch obj.(type) {
					case *configurationv1alpha1.KongCertificate:
						hasCert = true
					case *configurationv1alpha1.KongSNI:
						hasSNI = true
					}
				}
				require.True(t, hasCert, "should create KongCertificate")
				require.True(t, hasSNI, "should create KongSNI")
			},
		},
		{
			name: "skips certificate with unsupported group",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Group: ptrTo(gatewayv1.Group("custom.example.com")),
				Name:  "custom-cert",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// No mocks needed for this test.
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create any resources for unsupported group")
			},
		},
		{
			name: "skips certificate with unsupported kind",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Kind: ptrTo(gatewayv1.Kind("ConfigMap")),
				Name: "custom-cert",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// No mocks needed for this test.
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create any resources for unsupported kind")
			},
		},
		{
			name: "skips non-existent secret",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "non-existent-secret",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Don't create the secret.
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create resources for non-existent secret")
			},
		},
		{
			name: "returns error for invalid TLS secret",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "invalid-secret",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create an invalid TLS secret (missing required fields).
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						// Missing tls.crt and tls.key.
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: true,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create resources for invalid secret")
			},
		},
		{
			name: "skips certificate with empty group string (corev1)",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Group: ptrTo(gatewayv1.Group("")),
				Name:  "tls-secret",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": cert,
						"tls.key": key,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2, "should create resources for empty group (core)")
			},
		},
		{
			name: "skips certificate when cross-namespace reference not granted",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name:      "tls-secret",
				Namespace: ptrTo(gatewayv1.Namespace("cert-namespace")),
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "cert-namespace",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": cert,
						"tls.key": key,
					},
				}
				// No ReferenceGrant created, so access should be denied
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: false,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create resources when cross-namespace reference not granted")
			},
		},
		{
			name: "returns error when secret Get fails with non-NotFound error",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "tls-secret",
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Don't create the secret, interceptor will return error.
			},
			interceptorFunc: &interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "tls-secret" && key.Namespace == "default" {
						return fmt.Errorf("simulated API error")
					}
					return cl.Get(ctx, key, obj, opts...)
				},
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: true,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create resources when Get fails")
			},
		},
		{
			name: "returns error when ReferenceGrant check fails",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			listener: &gwtypes.Listener{
				Name: "https",
				Port: 443,
			},
			certRef: gatewayv1.SecretObjectReference{
				Name: "tls-secret",
				// Cross-namespace to trigger ReferenceGrant check.
				Namespace: ptrTo(gatewayv1.Namespace("other-namespace")),
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "other-namespace",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": cert,
						"tls.key": key,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			interceptorFunc: &interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					// Simulate error when checking ReferenceGrants.
					return fmt.Errorf("simulated ReferenceGrant check error")
				},
			},
			controlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "test-cp",
				},
			},
			expectError: true,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create resources when ReferenceGrant check fails")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Get())

			if tt.interceptorFunc != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(*tt.interceptorFunc)
			}

			fakeClient := clientBuilder.Build()

			tt.setupMocks(t, fakeClient)

			converter := newGatewayConverter(tt.gateway, fakeClient).(*gatewayConverter)
			converter.controlPlaneRef = tt.controlPlaneRef

			err := converter.processListenerCertificate(
				context.Background(),
				logr.Discard(),
				tt.listener,
				tt.certRef,
			)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			tt.validateOutput(t, converter.outputStore)
		})
	}
}

func TestTranslate(t *testing.T) {
	const (
		httpsPort     = gatewayv1.PortNumber(443)
		httpsProtocol = gatewayv1.HTTPSProtocolType
		httpProtocol  = gatewayv1.HTTPProtocolType
	)
	var (
		testCert    = []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")
		testKey     = []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----")
		tlsModeTerm = gatewayv1.TLSModeTerminate
	)

	tests := []struct {
		name           string
		gateway        *gwtypes.Gateway
		setupMocks     func(t *testing.T, cl client.Client)
		expectError    bool
		expectedCount  int
		errorContains  string
		validateOutput func(t *testing.T, objects []client.Object)
	}{
		{
			name: "translates gateway with single TLS listener",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "gateway-uid-1",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https",
							Port:     httpsPort,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Name: "tls-secret",
									},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass
				gwClass := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwClass))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension owned by Gateway.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "test-gateway",
								UID:        "gateway-uid-1",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create TLS Secret.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": testCert,
						"tls.key": testKey,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			expectError:   false,
			expectedCount: 2, // KongCertificate + KongSNI.
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2, "should create KongCertificate and KongSNI")

				var cert *configurationv1alpha1.KongCertificate
				var sni *configurationv1alpha1.KongSNI

				for _, obj := range objects {
					switch o := obj.(type) {
					case *configurationv1alpha1.KongCertificate:
						cert = o
					case *configurationv1alpha1.KongSNI:
						sni = o
					}
				}

				require.NotNil(t, cert, "KongCertificate should be created")
				require.NotNil(t, sni, "KongSNI should be created")
				require.Equal(t, "cert.test-gateway.443", cert.Name)
				require.Equal(t, "cert.test-gateway.443", sni.Spec.CertificateRef.Name)
			},
		},
		{
			name: "translates gateway with multiple TLS listeners",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-gateway",
					Namespace: "default",
					UID:       "gateway-uid-2",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https-443",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret-443"},
								},
							},
						},
						{
							Name:     "https-8443",
							Port:     8443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret-8443"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass
				gwClass := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwClass))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "multi-gateway",
								UID:        "gateway-uid-2",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create TLS Secrets.
				for _, name := range []string{"tls-secret-443", "tls-secret-8443"} {
					secret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: "default",
						},
						Type: corev1.SecretTypeTLS,
						Data: map[string][]byte{
							"tls.crt": testCert,
							"tls.key": testKey,
						},
					}
					require.NoError(t, cl.Create(context.Background(), secret))
				}
			},
			expectError:   false,
			expectedCount: 4, // 2 KongCertificates + 2 KongSNIs.
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 4, "should create 2 KongCertificates and 2 KongSNIs")
			},
		},
		{
			name: "skips non-TLS listeners",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mixed-gateway",
					Namespace: "default",
					UID:       "gateway-uid-3",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Port:     80,
							Protocol: httpProtocol,
						},
						{
							Name:     "https",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "mixed-gateway",
								UID:        "gateway-uid-3",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create TLS Secret.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": testCert,
						"tls.key": testKey,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			expectError:   false,
			expectedCount: 2, // Only 1 KongCertificate + 1 KongSNI (http listener skipped).
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2, "should create only 1 KongCertificate and 1 KongSNI")
			},
		},
		{
			name: "fails when gateway does not reference control plane",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-cp-gateway",
					Namespace: "default",
					UID:       "gateway-uid-4",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "tls-secret"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// No KonnectExtension created, gateway doesn't reference control plane.
			},
			expectError:   false, // Changed: unsupported gateways are silently skipped
			expectedCount: 0,
		},
		{
			name: "skips missing secrets without error",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "error-gateway",
					Namespace: "default",
					UID:       "gateway-uid-5",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https-1",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "missing-secret-1"},
								},
							},
						},
						{
							Name:     "https-2",
							Port:     8443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "missing-secret-2"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "error-gateway",
								UID:        "gateway-uid-5",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				// No secrets created, both listeners should be silently skipped.
				require.NoError(t, cl.Create(context.Background(), ext))
			},
			expectError:   false,
			expectedCount: 0,
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create any resources when secrets are missing")
			},
		},
		{
			name: "partial success, silently skips missing secret",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-gateway",
					Namespace: "default",
					UID:       "gateway-uid-6",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https-valid",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "valid-secret"},
								},
							},
						},
						{
							Name:     "https-invalid",
							Port:     8443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "missing-secret"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "partial-gateway",
								UID:        "gateway-uid-6",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create only the valid secret.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": testCert,
						"tls.key": testKey,
					},
				}
				require.NoError(t, cl.Create(context.Background(), secret))
			},
			expectError:   false,
			expectedCount: 2, // Creates resources only for valid listener.
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2, "should create resources for valid listener, skip missing secret")
			},
		},
		{
			name: "accumulates errors from multiple invalid secrets",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-secrets-gateway",
					Namespace: "default",
					UID:       "gateway-uid-7",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https-invalid-1",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "invalid-secret-1"},
								},
							},
						},
						{
							Name:     "https-invalid-2",
							Port:     8443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "invalid-secret-2"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "invalid-secrets-gateway",
								UID:        "gateway-uid-7",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create invalid secrets (missing required fields).
				for _, name := range []string{"invalid-secret-1", "invalid-secret-2"} {
					secret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: "default",
						},
						Type: corev1.SecretTypeTLS,
						Data: map[string][]byte{
							// Missing tls.crt and tls.key, will trigger validation error.
						},
					}
					require.NoError(t, cl.Create(context.Background(), secret))
				}
			},
			expectError:   true,
			expectedCount: 0,
			errorContains: "failed to process 2 listener(s)",
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 0, "should not create any resources when all secrets are invalid")
			},
		},
		{
			name: "partial failure, one valid one invalid secret",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mixed-validity-gateway",
					Namespace: "default",
					UID:       "gateway-uid-8",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
					Listeners: []gatewayv1.Listener{
						{
							Name:     "https-valid",
							Port:     443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "valid-secret"},
								},
							},
						},
						{
							Name:     "https-invalid",
							Port:     8443,
							Protocol: httpsProtocol,
							TLS: &gatewayv1.ListenerTLSConfig{
								Mode: &tlsModeTerm,
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{Name: "invalid-secret"},
								},
							},
						},
					},
				},
			},
			setupMocks: func(t *testing.T, cl client.Client) {
				// Create GatewayClass.
				gwc := &gatewayv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gatewayv1.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}
				require.NoError(t, cl.Create(context.Background(), gwc))

				// Create KonnectGatewayControlPlane.
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				}
				require.NoError(t, cl.Create(context.Background(), cp))

				// Create KonnectExtension.
				ext := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "mixed-validity-gateway",
								UID:        "gateway-uid-8",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}
				require.NoError(t, cl.Create(context.Background(), ext))

				// Create valid secret.
				validSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						"tls.crt": testCert,
						"tls.key": testKey,
					},
				}
				require.NoError(t, cl.Create(context.Background(), validSecret))

				// Create invalid secret (missing required fields).
				invalidSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "invalid-secret",
						Namespace: "default",
					},
					Type: corev1.SecretTypeTLS,
					Data: map[string][]byte{
						// Missing tls.crt and tls.key.
					},
				}
				require.NoError(t, cl.Create(context.Background(), invalidSecret))
			},
			expectError:   true,
			expectedCount: 2, // Valid listener still creates resources.
			errorContains: "failed to process 1 listener(s)",
			validateOutput: func(t *testing.T, objects []client.Object) {
				require.Len(t, objects, 2, "should create resources for valid listener despite one failure")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				Build()

			tt.setupMocks(t, fakeClient)

			converter := newGatewayConverter(tt.gateway, fakeClient)
			count, err := converter.Translate(context.Background(), logr.Discard())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expectedCount, count)

			if tt.validateOutput != nil {
				// Access outputStore directly from the converter.
				gc := converter.(*gatewayConverter)
				tt.validateOutput(t, gc.outputStore)
			}
		})
	}
}

// badObject is a fake client.Object that always fails conversion to unstructured.
type badObject struct {
	metav1.ObjectMeta

	Name string
}

func (b *badObject) GetObjectKind() schema.ObjectKind            { return schema.EmptyObjectKind }
func (b *badObject) DeepCopyObject() runtime.Object              { return b }
func (b *badObject) GetName() string                             { return b.Name }
func (b *badObject) SetName(name string)                         { b.Name = name }
func (b *badObject) GetNamespace() string                        { return "default" }
func (b *badObject) SetNamespace(ns string)                      {}
func (b *badObject) GetLabels() map[string]string                { return nil }
func (b *badObject) SetLabels(labels map[string]string)          {}
func (b *badObject) GetAnnotations() map[string]string           { return nil }
func (b *badObject) SetAnnotations(ann map[string]string)        {}
func (b *badObject) GetFinalizers() []string                     { return nil }
func (b *badObject) SetFinalizers(f []string)                    {}
func (b *badObject) GetOwnerReferences() []metav1.OwnerReference { return nil }
func (b *badObject) SetOwnerReferences([]metav1.OwnerReference)  {}

func TestGatewayConverter_GetOutputStore(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	validObj := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert1",
			Namespace: "default",
		},
	}
	validObj2 := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sni1",
			Namespace: "default",
		},
	}

	t.Run("all objects convert successfully", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newGatewayConverter(&gwtypes.Gateway{}, fakeClient).(*gatewayConverter)
		converter.outputStore = []client.Object{validObj, validObj2}
		objs, err := converter.GetOutputStore(ctx, logger)
		require.NoError(t, err)
		require.Len(t, objs, 2)
		require.Equal(t, "cert1", objs[0].GetName())
		require.Equal(t, "sni1", objs[1].GetName())
	})

	t.Run("one object fails conversion", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newGatewayConverter(&gwtypes.Gateway{}, fakeClient).(*gatewayConverter)
		badObj := &badObject{Name: "bad1"}
		converter.outputStore = []client.Object{validObj, badObj, validObj2}
		objs, err := converter.GetOutputStore(ctx, logger)
		require.Error(t, err)
		require.Len(t, objs, 2)
		require.Equal(t, "cert1", objs[0].GetName())
		require.Equal(t, "sni1", objs[1].GetName())
		require.Contains(t, err.Error(), "output store conversion failed with 1 errors")
		require.Contains(t, err.Error(), "failed to convert *converter.badObject bad1 to unstructured")
	})

	t.Run("all objects fail conversion", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		converter := newGatewayConverter(&gwtypes.Gateway{}, fakeClient).(*gatewayConverter)
		badObj1 := &badObject{Name: "bad1"}
		badObj2 := &badObject{Name: "bad2"}
		converter.outputStore = []client.Object{badObj1, badObj2}
		objs, err := converter.GetOutputStore(ctx, logger)
		require.Error(t, err)
		require.Len(t, objs, 0)
		require.Contains(t, err.Error(), "output store conversion failed with 2 errors")
		require.Contains(t, err.Error(), "failed to convert *converter.badObject bad1 to unstructured")
		require.Contains(t, err.Error(), "failed to convert *converter.badObject bad2 to unstructured")
	})
	tests := []struct {
		name    string
		gateway *gwtypes.Gateway
	}{
		{
			name: "creates converter with empty gateway",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
		},
		{
			name: "creates converter with gateway with listeners",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway-with-listeners",
					Namespace: "test-ns",
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name: "https",
							Port: 443,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			converter := newGatewayConverter(tt.gateway, fakeClient)

			require.NotNil(t, converter)

			// Verify internal state through GetRootObject
			rootObj := converter.GetRootObject()
			require.Equal(t, tt.gateway.Name, rootObj.Name)
			require.Equal(t, tt.gateway.Namespace, rootObj.Namespace)

			// Verify expected GVKs
			expectedGVKs := converter.GetExpectedGVKs()
			require.Len(t, expectedGVKs, 2)
			require.Contains(t, expectedGVKs, schema.GroupVersionKind{
				Group:   configurationv1alpha1.GroupVersion.Group,
				Version: configurationv1alpha1.GroupVersion.Version,
				Kind:    "KongCertificate",
			})
			require.Contains(t, expectedGVKs, schema.GroupVersionKind{
				Group:   configurationv1alpha1.GroupVersion.Group,
				Version: configurationv1alpha1.GroupVersion.Version,
				Kind:    "KongSNI",
			})
		})
	}
}

// Helper function to create pointer to value.
func ptrTo[T any](v T) *T {
	return &v
}

func TestHandleOrphanedResource(t *testing.T) {
	tests := []struct {
		name               string
		gatewayUID         string
		resource           func(gatewayUID string) map[string]any
		expectedSkipDelete bool
		expectError        bool
	}{
		{
			name:       "resource owned by this gateway - should not skip delete",
			gatewayUID: "gateway-uid-123",
			resource: func(gatewayUID string) map[string]any {
				return map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongCertificate",
					"metadata": map[string]any{
						"name":      "test-cert",
						"namespace": "default",
						"ownerReferences": []any{
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "test-gateway",
								"uid":        gatewayUID,
							},
						},
					},
				}
			},
			expectedSkipDelete: false,
			expectError:        false,
		},
		{
			name:       "resource not owned by this gateway - should skip delete",
			gatewayUID: "gateway-uid-123",
			resource: func(gatewayUID string) map[string]any {
				return map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongCertificate",
					"metadata": map[string]any{
						"name":      "test-cert",
						"namespace": "default",
						"ownerReferences": []any{
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "other-gateway",
								"uid":        "other-uid-456",
							},
						},
					},
				}
			},
			expectedSkipDelete: true,
			expectError:        false,
		},
		{
			name:       "resource with no owner references - should skip delete",
			gatewayUID: "gateway-uid-123",
			resource: func(gatewayUID string) map[string]any {
				return map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongCertificate",
					"metadata": map[string]any{
						"name":      "test-cert",
						"namespace": "default",
					},
				}
			},
			expectedSkipDelete: true,
			expectError:        false,
		},
		{
			name:       "resource with multiple owners including this gateway - should not skip delete",
			gatewayUID: "gateway-uid-123",
			resource: func(gatewayUID string) map[string]any {
				return map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongSNI",
					"metadata": map[string]any{
						"name":      "test-sni",
						"namespace": "default",
						"ownerReferences": []any{
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "other-gateway",
								"uid":        "other-uid-456",
							},
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "test-gateway",
								"uid":        gatewayUID,
							},
						},
					},
				}
			},
			expectedSkipDelete: false,
			expectError:        false,
		},
		{
			name:       "resource with multiple owners not including this gateway - should skip delete",
			gatewayUID: "gateway-uid-123",
			resource: func(gatewayUID string) map[string]any {
				return map[string]any{
					"apiVersion": "configuration.konghq.com/v1alpha1",
					"kind":       "KongSNI",
					"metadata": map[string]any{
						"name":      "test-sni",
						"namespace": "default",
						"ownerReferences": []any{
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "other-gateway-1",
								"uid":        "other-uid-1",
							},
							map[string]any{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "Gateway",
								"name":       "other-gateway-2",
								"uid":        "other-uid-2",
							},
						},
					},
				}
			},
			expectedSkipDelete: true,
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			logger := logr.Discard()

			gateway := &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "gateway-uid-123",
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				Build()

			converter := newGatewayConverter(gateway, fakeClient).(*gatewayConverter)

			resourceMap := tt.resource(tt.gatewayUID)
			unstructuredObj := &unstructured.Unstructured{}
			unstructuredObj.SetUnstructuredContent(resourceMap)

			skipDelete, err := converter.HandleOrphanedResource(ctx, logger, unstructuredObj)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedSkipDelete, skipDelete,
					"skipDelete should be %v but got %v", tt.expectedSkipDelete, skipDelete)
			}
		})
	}
}
