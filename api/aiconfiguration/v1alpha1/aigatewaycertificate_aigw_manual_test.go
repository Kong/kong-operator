package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayCertificate_ToAIGWCertificate covers the cert/key/certAlt/keyAlt secretRef
// resolution, the managed_by drop, and the cross-namespace secretRef rejection.
func TestAIGatewayCertificate_ToAIGWCertificate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "tls-secret", Namespace: "default",
			Data: map[string][]byte{
				"tls.crt": []byte("-----BEGIN CERTIFICATE-----"),
				"tls.key": []byte("-----BEGIN PRIVATE KEY-----"),
			},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayCertificate
		objects []runtime.Object
		want    *aigw.Certificate
		wantErr string
	}{
		{
			name: "inline cert/key, labels kept, managed_by dropped",
			obj: &AIGatewayCertificate{
				Name: "sample-ai-gw-cert", Namespace: "default",
				Spec: AIGatewayCertificateSpec{
					APISpec: AIGatewayCertificateAPISpec{
						Name:      "tls-cert",
						Labels:    PublicLabels{"app": "test1", "env": "test"},
						ManagedBy: ManagedBy{"kong-operator": "true"},
						Cert:      SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----")},
						Key:       SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN PRIVATE KEY-----")},
					},
				},
			},
			want: &aigw.Certificate{
				Name:   "tls-cert",
				Cert:   "-----BEGIN CERTIFICATE-----",
				Key:    "-----BEGIN PRIVATE KEY-----",
				Labels: aigw.Labels{"app": "test1", "env": "test"},
			},
		},
		{
			name: "cert/key resolved from a Secret, certAlt/keyAlt inlined",
			obj: &AIGatewayCertificate{
				Name: "sample-ai-gw-cert-secret", Namespace: "default",
				Spec: AIGatewayCertificateSpec{
					APISpec: AIGatewayCertificateAPISpec{
						Name: "tls-cert-from-secret",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "tls-secret", Key: "tls.crt"}},
						Key:  SensitiveDataSource{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "tls-secret", Key: "tls.key"}},
						CertAlt: SensitiveDataSource{
							Type:  SensitiveDataSourceTypeInline,
							Value: new("-----BEGIN EC CERTIFICATE-----"),
						},
						KeyAlt: SensitiveDataSource{
							Type:  SensitiveDataSourceTypeInline,
							Value: new("-----BEGIN EC PRIVATE KEY-----"),
						},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret()},
			want: &aigw.Certificate{
				Name:    "tls-cert-from-secret",
				Cert:    "-----BEGIN CERTIFICATE-----",
				Key:     "-----BEGIN PRIVATE KEY-----",
				CertAlt: "-----BEGIN EC CERTIFICATE-----",
				KeyAlt:  "-----BEGIN EC PRIVATE KEY-----",
			},
		},
		{
			name: "cross-namespace secretRef rejected",
			obj: &AIGatewayCertificate{
				Name: "sample-ai-gw-cert-cross-ns", Namespace: "default",
				Spec: AIGatewayCertificateSpec{
					APISpec: AIGatewayCertificateAPISpec{
						Name: "cross-ns-cert",
						Cert: SensitiveDataSource{
							Type: SensitiveDataSourceTypeSecretRef,
							SecretRef: &SensitiveDataSecretRef{
								Name:      "tls-secret",
								Key:       "tls.crt",
								Namespace: new("other-namespace"),
							},
						},
					},
				},
			},
			wantErr: "cross-namespace secretRef",
		},
		{
			name: "unset spec.apiSpec rejected",
			obj: &AIGatewayCertificate{
				Name: "sample-ai-gw-cert-no-apispec", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "missing secret",
			obj: &AIGatewayCertificate{
				Name: "sample-ai-gw-cert-missing-secret", Namespace: "default",
				Spec: AIGatewayCertificateSpec{
					APISpec: AIGatewayCertificateAPISpec{
						Name: "missing-secret-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "does-not-exist", Key: "tls.crt"}},
					},
				},
			},
			wantErr: "does-not-exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWCertificate(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayCertificate_ToAIGWCertificate_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWCertificatePayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.Certificate doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayCertificate_ToAIGWCertificate_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayCertificateAPISpec{
		Name: "tls-cert",
		Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----")},
		Key:  SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN PRIVATE KEY-----")},
		CertAlt: SensitiveDataSource{
			Type:  SensitiveDataSourceTypeInline,
			Value: new("-----BEGIN EC CERTIFICATE-----"),
		},
		KeyAlt: SensitiveDataSource{
			Type:  SensitiveDataSourceTypeInline,
			Value: new("-----BEGIN EC PRIVATE KEY-----"),
		},
		Labels:    PublicLabels{"app": "test1"},
		ManagedBy: ManagedBy{"kong-operator": "true"},
	}
	data, err := spec.marshalAIGWCertificatePayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cert aigw.Certificate
	require.NoError(t, dec.Decode(&cert))

	require.Equal(t, "tls-cert", cert.Name)
	require.Equal(t, "-----BEGIN CERTIFICATE-----", cert.Cert)
	require.Equal(t, "-----BEGIN PRIVATE KEY-----", cert.Key)
	require.Equal(t, "-----BEGIN EC CERTIFICATE-----", cert.CertAlt)
	require.Equal(t, "-----BEGIN EC PRIVATE KEY-----", cert.KeyAlt)
	require.Equal(t, aigw.Labels{"app": "test1"}, cert.Labels)
}
