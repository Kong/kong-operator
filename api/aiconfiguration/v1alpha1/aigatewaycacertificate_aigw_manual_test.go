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

// TestAIGatewayCACertificate_ToAIGWCertificate covers the cert secretRef resolution, the
// managed_by drop, and the cross-namespace secretRef rejection.
func TestAIGatewayCACertificate_ToAIGWCertificate(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferencedSecret returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "ca-secret", Namespace: "default",
			Data: map[string][]byte{
				"ca.crt": []byte("-----BEGIN CERTIFICATE-----"),
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
						Cert:      SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("-----BEGIN CERTIFICATE-----")},
					},
				},
			},
			want: &aigw.CACertificate{
				Name:   "ca-cert",
				Cert:   "-----BEGIN CERTIFICATE-----",
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
				Cert: "-----BEGIN CERTIFICATE-----",
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

// TestAIGatewayCACertificate_ToAIGWCertificate_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWCACertificatePayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.CACertificate doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayCACertificate_ToAIGWCertificate_StrictRoundTrip(t *testing.T) {
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
