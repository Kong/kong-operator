package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// TestAIGatewaySNI_ToAIGWSNI covers the certificate reference resolution — deliberately
// without any SetKonnectID on the referenced certificate, pinning that the on-prem
// translation applies no Konnect-ID gate — the same-OnPremAIGateway membership check on the
// referenced certificate, the certificate CR ref strip, and the managed_by drop.
func TestAIGatewaySNI_ToAIGWSNI(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	// newReferencedCertificate returns a fresh object per subtest: the fake client's tracker
	// mutates the objects it's given (SetResourceVersion on Build), so parallel subtests
	// sharing one instance race.
	newReferencedCertificate := func() *AIGatewayCertificate {
		return &AIGatewayCertificate{
			Name: "ai-gw-cert", Namespace: "default",
			Spec: AIGatewayCertificateSpec{
				AIGatewayRef: AIGatewayRef{
					Group:         AIGatewayRefGroupOnPrem,
					Kind:          AIGatewayRefKindOnPrem,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
				},
				APISpec: AIGatewayCertificateAPISpec{
					Name: "aigw-cert",
				},
			},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewaySNI
		want    *aigw.SNI
		wantErr string
	}{
		{
			name: "certificate resolved to its entity name, labels kept, managed_by dropped",
			obj: &AIGatewaySNI{
				Name: "sample-ai-gw-sni", Namespace: "default",
				Spec: AIGatewaySNISpec{
					AIGatewayRef: AIGatewayRef{
						Group:         AIGatewayRefGroupOnPrem,
						Kind:          AIGatewayRefKindOnPrem,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
					},
					APISpec: AIGatewaySNIAPISpec{
						Name:        "api-example-com",
						DisplayName: "api.example.com",
						Hostname:    new(AIGatewayHostname("api.example.com")),
						Labels:      PublicLabels{"app": "test1", "env": "test"},
						ManagedBy:   ManagedBy{"kong-operator": "true"},
						Certificate: AIGatewayCertificateRef{Name: "ai-gw-cert"},
					},
				},
			},
			want: &aigw.SNI{
				Name:        "api-example-com",
				DisplayName: "api.example.com",
				Hostname:    "api.example.com",
				Labels:      aigw.Labels{"app": "test1", "env": "test"},
				Certificate: "aigw-cert",
			},
		},
		{
			name: "unset spec.apiSpec rejected",
			obj: &AIGatewaySNI{
				Name: "sample-ai-gw-sni-no-apispec", Namespace: "default",
			},
			wantErr: "spec.apiSpec is required",
		},
		{
			name: "dangling certificate reference",
			obj: &AIGatewaySNI{
				Name: "sample-ai-gw-sni-dangling", Namespace: "default",
				Spec: AIGatewaySNISpec{
					AIGatewayRef: AIGatewayRef{
						Group:         AIGatewayRefGroupOnPrem,
						Kind:          AIGatewayRefKindOnPrem,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
					},
					APISpec: AIGatewaySNIAPISpec{
						Name:        "dangling-sni",
						DisplayName: "Dangling SNI",
						Hostname:    new(AIGatewayHostname("dangling.example.com")),
						Certificate: AIGatewayCertificateRef{Name: "does-not-exist"},
					},
				},
			},
			wantErr: "does-not-exist",
		},
		{
			name: "cross-namespace certificate reference rejected",
			obj: &AIGatewaySNI{
				Name: "sample-ai-gw-sni-cross-ns", Namespace: "default",
				Spec: AIGatewaySNISpec{
					AIGatewayRef: AIGatewayRef{
						Group:         AIGatewayRefGroupOnPrem,
						Kind:          AIGatewayRefKindOnPrem,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
					},
					APISpec: AIGatewaySNIAPISpec{
						Name:        "cross-ns-sni",
						DisplayName: "Cross NS SNI",
						Hostname:    new(AIGatewayHostname("cross.example.com")),
						Certificate: AIGatewayCertificateRef{Name: "ai-gw-cert", Namespace: "other-namespace"},
					},
				},
			},
			wantErr: "cross-namespace reference",
		},
		{
			name: "certificate targeting a different OnPremAIGateway rejected",
			obj: &AIGatewaySNI{
				Name: "sample-ai-gw-sni-other-gw", Namespace: "default",
				Spec: AIGatewaySNISpec{
					AIGatewayRef: AIGatewayRef{
						Group:         AIGatewayRefGroupOnPrem,
						Kind:          AIGatewayRefKindOnPrem,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "other-gw"},
					},
					APISpec: AIGatewaySNIAPISpec{
						Name:        "other-gw-sni",
						DisplayName: "Other GW SNI",
						Hostname:    new(AIGatewayHostname("other.example.com")),
						Certificate: AIGatewayCertificateRef{Name: "ai-gw-cert"},
					},
				},
			},
			wantErr: "does not target the SNI's OnPremAIGateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newReferencedCertificate()).Build()
			got, err := tt.obj.ToAIGWSNI(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewaySNI_ToAIGWSNI_StrictRoundTrip guards against a dropped or renamed field: it
// decodes marshalAIGWSNIPayload's output with yaml.v3's KnownFields(true), which errors on
// any key aigw.SNI doesn't recognize. See aigatewaymodel_aigw_manual_test.go for the
// rationale.
func TestAIGatewaySNI_ToAIGWSNI_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewaySNIAPISpec{
		Name:        "api-example-com",
		DisplayName: "api.example.com",
		Hostname:    new(AIGatewayHostname("api.example.com")),
		Labels:      PublicLabels{"app": "test1"},
		ManagedBy:   ManagedBy{"kong-operator": "true"},
		// The certificate ref is stripped by the payload builder (it is re-attached resolved
		// by ToAIGWSNI), so the strict decode must not see it.
		Certificate: AIGatewayCertificateRef{Name: "ai-gw-cert"},
	}
	data, err := spec.marshalAIGWSNIPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var sni aigw.SNI
	require.NoError(t, dec.Decode(&sni))

	require.Equal(t, "api-example-com", sni.Name)
	require.Equal(t, "api.example.com", sni.DisplayName)
	require.Equal(t, "api.example.com", sni.Hostname)
	require.Equal(t, aigw.Labels{"app": "test1"}, sni.Labels)
	require.Empty(t, sni.Certificate)
}

// TestOnPremAIGatewayRefKey covers the ns/name key rendering that the SNI's certificate
// membership check compares against: the generated index extractor's branch for an
// explicit-namespace ref (the ref's namespace wins over the entity's own) has no other test.
func TestOnPremAIGatewayRefKey(t *testing.T) {
	t.Parallel()

	ref := AIGatewayRef{
		Group:         AIGatewayRefGroupOnPrem,
		Kind:          AIGatewayRefKindOnPrem,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
	}
	key, ok := onPremAIGatewayRefKey("default", ref)
	require.True(t, ok)
	require.Equal(t, "default/gw", key)

	// The explicit-namespace branch must agree with the generated index extractors: the ref's
	// namespace wins over the entity's own.
	ref.NamespacedRef.Namespace = new("default")
	key, ok = onPremAIGatewayRefKey("other", ref)
	require.True(t, ok)
	require.Equal(t, "default/gw", key)

	// A non-nil NamespacedRef whose Group and Kind stay at the Konnect default must not
	// resolve either: this is the half of the false branch the zero ref short-circuits past,
	// and the half resolveReferencedCertificate's certKey check hits for a certificate
	// targeting a KonnectAIGateway.
	konnectRef := AIGatewayRef{NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"}}
	_, ok = onPremAIGatewayRefKey("default", konnectRef)
	require.False(t, ok)

	_, ok = onPremAIGatewayRefKey("default", AIGatewayRef{})
	require.False(t, ok)
}
