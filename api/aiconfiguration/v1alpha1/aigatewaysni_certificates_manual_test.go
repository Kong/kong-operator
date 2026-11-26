package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// TestAIGatewaySNI_CertificateReferences pins the generated reference wiring of
// spec.apiSpec.certificate on the Konnect path: the ref resolves to the referenced
// certificate's Konnect name in the SDK request, a cross-namespace ref is reported as a
// KongReferenceGrant-checked sibling reference, and only a grant in the target namespace
// lets it through.
func TestAIGatewaySNI_CertificateReferences(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	newCertificate := func() *AIGatewayCertificate {
		cert := &AIGatewayCertificate{
			Name: "ai-gw-cert", Namespace: "certs",
			Spec: AIGatewayCertificateSpec{
				APISpec: AIGatewayCertificateAPISpec{Name: "aigw-cert"},
			},
		}
		cert.SetKonnectID("cert-id")
		return cert
	}
	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-sni", Namespace: "certs",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     GroupName,
					Kind:      "AIGatewaySNI",
					Namespace: "default",
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: GroupName,
					Kind:  "AIGatewayCertificate",
					Name:  new(configurationv1alpha1.ObjectName("ai-gw-cert")),
				},
			},
		},
	}

	newSNI := func(ref AIGatewayCertificateRef) *AIGatewaySNI {
		return &AIGatewaySNI{
			Name: "sni", Namespace: "default",
			Spec: AIGatewaySNISpec{
				APISpec: AIGatewaySNIAPISpec{
					Name:        "api-example-com",
					DisplayName: "api.example.com",
					Hostname:    new(AIGatewayHostname("api.example.com")),
					Certificate: ref,
				},
			},
		}
	}

	t.Run("cross-namespace ref without a grant is rejected", func(t *testing.T) {
		t.Parallel()

		obj := newSNI(AIGatewayCertificateRef{Name: "ai-gw-cert", Namespace: "certs"})

		checks := obj.CrossNamespaceSiblingReferences()
		require.Len(t, checks, 1)
		assert.Equal(t, "AIGatewaySNI", checks[0].FromGVK.Kind)
		assert.Equal(t, "AIGatewayCertificate", checks[0].ToGVK.Kind)
		assert.Equal(t, "default", checks[0].FromNamespace)
		assert.Equal(t, "certs", checks[0].ToNamespace)
		assert.Equal(t, "ai-gw-cert", checks[0].ToName)

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newCertificate()).Build()
		err := crossnamespace.CheckSiblingReferences(t.Context(), cl, obj)
		require.Error(t, err)
		assert.True(t, crossnamespace.IsReferenceNotGranted(err), "want a not-granted error, got %v", err)
	})

	t.Run("cross-namespace ref with a grant resolves the Konnect name", func(t *testing.T) {
		t.Parallel()

		obj := newSNI(AIGatewayCertificateRef{Name: "ai-gw-cert", Namespace: "certs"})
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newCertificate(), grant).Build()

		require.NoError(t, crossnamespace.CheckSiblingReferences(t.Context(), cl, obj))

		req, err := obj.ToCreateAIGatewaySNIRequest(t.Context(), cl)
		require.NoError(t, err)

		body, err := json.Marshal(req)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "aigw-cert", got["certificate"])
	})

	t.Run("same-namespace ref needs no grant", func(t *testing.T) {
		t.Parallel()

		obj := newSNI(AIGatewayCertificateRef{Name: "ai-gw-cert"})
		obj.Namespace = "certs"
		assert.Empty(t, obj.CrossNamespaceSiblingReferences())

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newCertificate()).Build()
		require.NoError(t, crossnamespace.CheckSiblingReferences(t.Context(), cl, obj))

		req, err := obj.ToCreateAIGatewaySNIRequest(t.Context(), cl)
		require.NoError(t, err)
		assert.Equal(t, "aigw-cert", req.Certificate)
	})
}
