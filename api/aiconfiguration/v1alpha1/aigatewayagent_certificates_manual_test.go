package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayAgent_CertificateReferences pins the generated reference wiring of
// spec.apiSpec.config.caCertificates and .certificates: the refs resolve to the referenced
// entities' Konnect names in the SDK request, and only a cross-namespace ref is reported as a
// KongReferenceGrant-checked sibling reference (supportCrossNamespaceReference).
func TestAIGatewayAgent_CertificateReferences(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	caCert := &AIGatewayCACertificate{
		Name: "ai-gw-ca-cert", Namespace: "certs",
		Spec: AIGatewayCACertificateSpec{
			APISpec: AIGatewayCACertificateAPISpec{Name: "aigw-ca-cert"},
		},
	}
	caCert.SetKonnectID("ca-cert-id")
	cert := &AIGatewayCertificate{
		Name: "ai-gw-cert", Namespace: "default",
		Spec: AIGatewayCertificateSpec{
			APISpec: AIGatewayCertificateAPISpec{Name: "aigw-cert"},
		},
	}
	cert.SetKonnectID("cert-id")

	obj := &AIGatewayAgent{
		Name: "agent", Namespace: "default",
		Spec: AIGatewayAgentSpec{
			APISpec: AIGatewayAgentAPISpec{
				Name: "agent",
				Type: "http",
				Config: AIGatewayAgentConfig{
					URL:            "https://agent.example",
					CaCertificates: []AIGatewayCACertificateRef{{Name: "ai-gw-ca-cert", Namespace: "certs"}},
					Certificates:   []AIGatewayCertificateRef{{Name: "ai-gw-cert"}},
				},
			},
		},
	}

	checks := obj.CrossNamespaceSiblingReferences()
	require.Len(t, checks, 1, "only the cross-namespace CA certificate ref needs a grant check")
	assert.Equal(t, "AIGatewayAgent", checks[0].FromGVK.Kind)
	assert.Equal(t, "AIGatewayCACertificate", checks[0].ToGVK.Kind)
	assert.Equal(t, "certs", checks[0].ToNamespace)
	assert.Equal(t, "ai-gw-ca-cert", checks[0].ToName)

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caCert, cert).Build()
	req, err := obj.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.NoError(t, err)

	body, err := json.Marshal(req)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	config, ok := got["config"].(map[string]any)
	require.True(t, ok, "request has no config object: %s", body)
	assert.Equal(t, []any{"aigw-ca-cert"}, config["ca_certificates"])
	assert.Equal(t, []any{"aigw-cert"}, config["certificates"])
}
