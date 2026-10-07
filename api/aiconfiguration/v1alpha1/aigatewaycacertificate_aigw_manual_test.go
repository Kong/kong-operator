package v1alpha1

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// genCACertPEM returns a self-signed CA certificate PEM with the given validity window.
func genCACertPEM(t *testing.T, notBefore, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// TestAIGatewayCACertificate_ToAIGWCACertificate covers the cert secretRef resolution, the
// managed_by drop, the cross-namespace secretRef rejection, and the cert validation that
// keeps an unparsable or expired cert from failing the whole DB-less push.
func TestAIGatewayCACertificate_ToAIGWCACertificate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	now := time.Now()
	validCert := genCACertPEM(t, now.Add(-time.Hour), now.AddDate(1, 0, 0))
	expiredCert := genCACertPEM(t, now.AddDate(-2, 0, 0), now.AddDate(-1, 0, 0))
	notYetValidCert := genCACertPEM(t, now.Add(time.Hour), now.AddDate(2, 0, 0))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "ca-secret", Namespace: "default",
			Data: map[string][]byte{
				"ca.crt": []byte(validCert),
			},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayCACertificate
		objects []runtime.Object
		want    *aigw.CACertificate
		wantErr string
	}{
		{
			name: "inline cert, labels kept, managed_by dropped",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name:      "ca-cert",
						Labels:    PublicLabels{"app": "test1", "env": "test"},
						ManagedBy: ManagedBy{"kong-operator": "true"},
						Cert:      SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new(validCert)},
					},
				},
			},
			want: &aigw.CACertificate{
				Name:   "ca-cert",
				Cert:   validCert,
				Labels: aigw.Labels{"app": "test1", "env": "test"},
			},
		},
		{
			name: "cert resolved from a Secret",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-secret", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "ca-cert-from-secret",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "ca-secret", Key: "ca.crt"}},
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret()},
			want: &aigw.CACertificate{
				Name: "ca-cert-from-secret",
				Cert: validCert,
			},
		},
		{
			name: "cross-namespace secretRef rejected",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-cross-ns", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "cross-ns-ca-cert",
						Cert: SensitiveDataSource{
							Type: SensitiveDataSourceTypeSecretRef,
							SecretRef: &SensitiveDataSecretRef{
								Name:      "ca-secret",
								Key:       "ca.crt",
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
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-no-apispec", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "missing secret",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-missing-secret", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "missing-secret-ca-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeSecretRef, SecretRef: &SensitiveDataSecretRef{Name: "does-not-exist", Key: "ca.crt"}},
					},
				},
			},
			wantErr: "does-not-exist",
		},
		{
			name: "unparsable cert rejected",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-unparsable", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "unparsable-ca-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----\nnot-a-certificate\n-----END CERTIFICATE-----\n")},
					},
				},
			},
			wantErr: "invalid cert",
		},
		{
			name: "cert without any PEM block rejected",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-no-pem", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "no-pem-ca-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("not a PEM at all")},
					},
				},
			},
			wantErr: "no PEM-encoded CERTIFICATE block found",
		},
		{
			name: "expired cert rejected",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-expired", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "expired-ca-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new(expiredCert)},
					},
				},
			},
			wantErr: "expired",
		},
		{
			name: "not yet valid cert rejected",
			obj: &AIGatewayCACertificate{
				Name: "sample-ai-gw-ca-cert-not-yet-valid", Namespace: "default",
				Spec: AIGatewayCACertificateSpec{
					APISpec: AIGatewayCACertificateAPISpec{
						Name: "not-yet-valid-ca-cert",
						Cert: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new(notYetValidCert)},
					},
				},
			},
			wantErr: "not valid before",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...).Build()
			got, err := tt.obj.ToAIGWCACertificate(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayCACertificate_ToAIGWCACertificate_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWCACertificatePayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.CACertificate doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayCACertificate_ToAIGWCACertificate_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayCACertificateAPISpec{
		Name:      "ca-cert",
		Cert:      SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----")},
		Labels:    PublicLabels{"app": "test1"},
		ManagedBy: ManagedBy{"kong-operator": "true"},
	}
	data, err := spec.marshalAIGWCACertificatePayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var caCert aigw.CACertificate
	require.NoError(t, dec.Decode(&caCert))

	require.Equal(t, "ca-cert", caCert.Name)
	require.Equal(t, "-----BEGIN CERTIFICATE-----", caCert.Cert)
	require.Equal(t, aigw.Labels{"app": "test1"}, caCert.Labels)
}
