/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package translator

import (
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
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func aiGatewayModelFixture(name string) *aiconfigurationv1alpha1.AIGatewayModel {
	return &aiconfigurationv1alpha1.AIGatewayModel{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
						Formats:     []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{Paths: []string{"/" + name}},
						},
					},
				},
			},
		},
	}
}

func aiGatewayModelProviderFixture(name string) *aiconfigurationv1alpha1.AIGatewayModelProvider {
	return &aiconfigurationv1alpha1.AIGatewayModelProvider{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayModelProviderSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayModelProviderAPISpec{
				AIGatewayModelProviderConfig: &aiconfigurationv1alpha1.AIGatewayModelProviderConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelProviderConfigTypeOpenai,
					Openai: &aiconfigurationv1alpha1.AIGatewayModelProviderOpenai{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
					},
				},
			},
		},
	}
}

func aiGatewayPolicyFixture(name string) *aiconfigurationv1alpha1.AIGatewayPolicy {
	return &aiconfigurationv1alpha1.AIGatewayPolicy{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
				Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				DisplayName: name,
				Type:        "rate-limiting",
			},
		},
	}
}

func aiGatewayConsumerGroupFixture(name string) *aiconfigurationv1alpha1.AIGatewayConsumerGroup {
	return &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerGroupSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerGroupAPISpec{
				Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				DisplayName: name,
			},
		},
	}
}

func aiGatewayAuthStrategyFixture(name string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	return &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeKeyAuth,
					KeyAuth: &aiconfigurationv1alpha1.AIGatewayAuthStrategyKeyAuth{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
					},
				},
			},
		},
	}
}

func aiGatewayConsumerFixture(name string) *aiconfigurationv1alpha1.AIGatewayConsumer {
	return &aiconfigurationv1alpha1.AIGatewayConsumer{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerAPISpec{
				Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				DisplayName: name,
				Type:        "api-key",
			},
		},
	}
}

// genCACertPEM returns a self-signed CA certificate PEM, valid from an hour ago for a year.
// ToAIGWCACertificate validates the cert (parse + validity window), so fixtures need real
// certificates; each call generates a fresh key, so two calls yield distinct cert content.
func genCACertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func aiGatewayCACertificateFixture(name, cert string) *aiconfigurationv1alpha1.AIGatewayCACertificate {
	return &aiconfigurationv1alpha1.AIGatewayCACertificate{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayCACertificateSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCACertificateAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(cert),
				},
			},
		},
	}
}

func aiGatewayCertificateFixture(name string) *aiconfigurationv1alpha1.AIGatewayCertificate {
	return &aiconfigurationv1alpha1.AIGatewayCertificate{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayCertificateSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCertificateAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new("-----BEGIN CERTIFICATE-----"),
				},
				Key: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new("-----BEGIN PRIVATE KEY-----"),
				},
			},
		},
	}
}

func aiGatewayCustomPolicyFixture(name string) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
	return &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled,
					Installed: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyInstalledRequest{
						Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						Schema: aiconfigurationv1alpha1.ConfigMapDataSource{
							Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
							Value: new("return { name = \"" + name + "\" }"),
						},
					},
				},
			},
		},
	}
}

func aiGatewaySNIFixture(name, certName string) *aiconfigurationv1alpha1.AIGatewaySNI {
	return &aiconfigurationv1alpha1.AIGatewaySNI{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewaySNISpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewaySNIAPISpec{
				Name:        name,
				DisplayName: name,
				Hostname:    new(aiconfigurationv1alpha1.AIGatewayHostname(name + ".example.com")),
				// Resolves to the referenced certificate's entity name (certName's spec name
				// equals its k8s name in the fixture).
				Certificate: aiconfigurationv1alpha1.AIGatewayCertificateRef{Name: certName},
			},
		},
	}
}

func aiGatewayAgentFixture(name string) *aiconfigurationv1alpha1.AIGatewayAgent {
	return &aiconfigurationv1alpha1.AIGatewayAgent{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayAgentSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayAgentAPISpec{
				Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
				DisplayName: name,
				Type:        "http",
				Config: aiconfigurationv1alpha1.AIGatewayAgentConfig{
					URL: "http://upstream:8080",
					// A route with no matcher matches every request, and the
					// converter warns about it (ROUTING_CONFLICT), which would
					// break the pristine-render no-warnings assertion below.
					Route: aiconfigurationv1alpha1.AIGatewayRouteConfig{Paths: []string{"/agent/" + name}},
				},
				// Resolves to the referenced entities' names (their spec names equal their
				// k8s names in the fixtures).
				Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-" + name[len(name)-1:]}},
				Access: aiconfigurationv1alpha1.AIGatewayAgentAccess{
					Acls: &aiconfigurationv1alpha1.AIGatewayAgentAccessAcls{
						Type:  aiconfigurationv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: "group-" + name[len(name)-1:]}}},
					},
					AuthStrategies: []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{{Name: "auth-strategy-" + name[len(name)-1:]}},
				},
			},
		},
	}
}

func aiGatewayMCPServerFixture(name string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	return &aiconfigurationv1alpha1.AIGatewayMCPServer{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
					// passthrough-listener, not conversion-only: an unexposed conversion-only
					// server is pruned by the converter with a warning, which would break the
					// pristine-render no-warnings assertion below.
					Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener,
					PassthroughListener: &aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListener{
						Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						Config: aiconfigurationv1alpha1.AIGatewayMCPServerWithUpstreamConfig{
							URL:   "http://upstream:8080",
							Route: aiconfigurationv1alpha1.AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/" + name}},
						},
						// Resolves to the referenced entity's name (its spec name equals its
						// k8s name in the fixtures).
						Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-" + name[len(name)-1:]}},
					},
				},
			},
		},
	}
}

// aiGatewayVaultFixture returns an env-backed vault. prefix is the vault prefix (the CR's
// apiSpec.env.name and the Kong entity's `prefix`), so passing the same one twice produces a
// duplicate-prefix document.
func aiGatewayVaultFixture(name, prefix string) *aiconfigurationv1alpha1.AIGatewayVault {
	return &aiconfigurationv1alpha1.AIGatewayVault{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayVaultSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayVaultAPISpec{
				AIGatewayVaultConfig: &aiconfigurationv1alpha1.AIGatewayVaultConfig{
					Type: aiconfigurationv1alpha1.AIGatewayVaultConfigTypeEnvironmentVariable,
					EnvironmentVariable: &aiconfigurationv1alpha1.EnvironmentVariableVault{
						Name:   aiconfigurationv1alpha1.AIGatewayVaultName(prefix),
						Config: aiconfigurationv1alpha1.EnvironmentVariableVaultConfig{Prefix: "MY_SECRET_"},
					},
				},
			},
		},
	}
}

// TestBuildDocument covers listing, conversion and deterministic ordering: translateKind sorts
// by k8s object name so the rendered payload (and its hash, which drives the drift loop in
// controller.go) doesn't flap across List calls that return in a different order.
func TestBuildDocument(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// Registered out of sort order to prove translateKind, not List, does the sorting.
	modelB := aiGatewayModelFixture("model-b")
	modelA := aiGatewayModelFixture("model-a")
	providerB := aiGatewayModelProviderFixture("provider-b")
	providerA := aiGatewayModelProviderFixture("provider-a")
	policyB := aiGatewayPolicyFixture("policy-b")
	policyA := aiGatewayPolicyFixture("policy-a")
	groupB := aiGatewayConsumerGroupFixture("group-b")
	groupA := aiGatewayConsumerGroupFixture("group-a")
	authStrategyB := aiGatewayAuthStrategyFixture("auth-strategy-b")
	authStrategyA := aiGatewayAuthStrategyFixture("auth-strategy-a")
	consumerB := aiGatewayConsumerFixture("consumer-b")
	consumerA := aiGatewayConsumerFixture("consumer-a")
	certB := aiGatewayCertificateFixture("cert-b")
	certA := aiGatewayCertificateFixture("cert-a")
	caCertB := aiGatewayCACertificateFixture("ca-cert-b", genCACertPEM(t))
	caCertA := aiGatewayCACertificateFixture("ca-cert-a", genCACertPEM(t))
	sniB := aiGatewaySNIFixture("sni-b", "cert-b")
	sniA := aiGatewaySNIFixture("sni-a", "cert-a")
	customPolicyB := aiGatewayCustomPolicyFixture("custom-policy-b")
	customPolicyA := aiGatewayCustomPolicyFixture("custom-policy-a")
	agentB := aiGatewayAgentFixture("agent-b")
	agentA := aiGatewayAgentFixture("agent-a")
	mcpServerB := aiGatewayMCPServerFixture("mcp-server-b")
	mcpServerA := aiGatewayMCPServerFixture("mcp-server-a")
	vaultB := aiGatewayVaultFixture("vault-b", "env-b")
	vaultA := aiGatewayVaultFixture("vault-a", "env-a")

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, modelB, modelA, providerB, providerA, policyB, policyA, groupB, groupA,
			authStrategyB, authStrategyA, consumerB, consumerA, certB, certA, sniB, sniA,
			customPolicyB, customPolicyA, caCertB, caCertA, agentB, agentA, mcpServerB, mcpServerA,
			vaultB, vaultA)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)
	require.Len(t, doc.Models, 2)
	require.Equal(t, "model-a", doc.Models[0].Name)
	require.Equal(t, "model-b", doc.Models[1].Name)
	require.Len(t, doc.ModelProviders, 2)
	require.Equal(t, "provider-a", doc.ModelProviders[0].Name)
	require.Equal(t, "provider-b", doc.ModelProviders[1].Name)
	require.Len(t, doc.Policies, 2)
	require.Equal(t, "policy-a", doc.Policies[0].Name)
	require.Equal(t, "policy-b", doc.Policies[1].Name)
	require.Len(t, doc.ConsumerGroups, 2)
	require.Equal(t, "group-a", doc.ConsumerGroups[0].Name)
	require.Equal(t, "group-b", doc.ConsumerGroups[1].Name)
	require.Len(t, doc.AuthStrategies, 2)
	require.Equal(t, "auth-strategy-a", doc.AuthStrategies[0].Name)
	require.Equal(t, "auth-strategy-b", doc.AuthStrategies[1].Name)
	require.Len(t, doc.Consumers, 2)
	require.Equal(t, "consumer-a", doc.Consumers[0].Name)
	require.Equal(t, "consumer-b", doc.Consumers[1].Name)
	require.Len(t, doc.Certificates, 2)
	require.Equal(t, "cert-a", doc.Certificates[0].Name)
	require.Equal(t, "cert-b", doc.Certificates[1].Name)
	require.Len(t, doc.CACertificates, 2)
	require.Equal(t, "ca-cert-a", doc.CACertificates[0].Name)
	require.Equal(t, "ca-cert-b", doc.CACertificates[1].Name)
	require.Len(t, doc.SNIs, 2)
	require.Equal(t, "sni-a", doc.SNIs[0].Name)
	require.Equal(t, "cert-a", doc.SNIs[0].Certificate)
	require.Equal(t, "sni-b", doc.SNIs[1].Name)
	require.Equal(t, "cert-b", doc.SNIs[1].Certificate)
	require.Len(t, doc.CustomPolicies, 2)
	require.Equal(t, "custom-policy-a", doc.CustomPolicies[0].Name)
	require.Equal(t, "custom-policy-b", doc.CustomPolicies[1].Name)
	require.Len(t, doc.Agents, 2)
	require.Equal(t, "agent-a", doc.Agents[0].Name)
	require.Equal(t, "agent-b", doc.Agents[1].Name)
	// The agents' policy/ACL/auth-strategy references resolve to the referenced entities'
	// names (their spec names equal their k8s names in the fixtures).
	require.Equal(t, []string{"policy-a"}, doc.Agents[0].Policies)
	require.Equal(t, []string{"group-a"}, doc.Agents[0].Access.ACLs.Allow)
	require.Equal(t, []string{"auth-strategy-a"}, doc.Agents[0].Access.AuthStrategies)
	require.Len(t, doc.MCPServers, 2)
	require.Equal(t, "mcp-server-a", doc.MCPServers[0].Name)
	require.Equal(t, "mcp-server-b", doc.MCPServers[1].Name)
	// The MCP servers' policy references resolve to the referenced entity's name and their
	// config.url lands on the top-level UpstreamURL.
	require.Equal(t, []string{"policy-a"}, doc.MCPServers[0].Policies)
	require.Equal(t, "http://upstream:8080", doc.MCPServers[0].UpstreamURL)
	// The vault's apiSpec name is its Kong prefix; the vault type is the Kong backend.
	require.Len(t, doc.Vaults, 2)
	require.Equal(t, "env-a", doc.Vaults[0].Name)
	require.Equal(t, "env", doc.Vaults[0].Type)
	require.Equal(t, map[string]any{"prefix": "MY_SECRET_"}, doc.Vaults[0].Config)
	require.Equal(t, "env-b", doc.Vaults[1].Name)

	// Every entity translated successfully, so all statuses are reported as such.
	require.Len(t, statuses, 26)
	for _, s := range statuses {
		require.NoError(t, s.Err)
	}

	// Render the pristine document before the dangling provider reference is injected
	// below. A converter failure to resolve an SNI's certificate surfaces only as a
	// warning issue, so require none.
	sniPayload, sniIssues, err := RenderDBLessYAML(doc, convert.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, sniPayload)
	require.Empty(t, sniIssues)
	// The CA certificates must reach the rendered payload with unique IDs: the converter
	// derives each entity ID from the cert content, so a tag-only match wouldn't catch an
	// ID collision. Parse the payload and check the rendered ca_certificates themselves.
	var dblessConfig struct {
		CACertificates []struct {
			ID   string   `yaml:"id"`
			Tags []string `yaml:"tags"`
		} `yaml:"ca_certificates"`
		Vaults []struct {
			ID     string         `yaml:"id"`
			Prefix string         `yaml:"prefix"`
			Name   string         `yaml:"name"`
			Config map[string]any `yaml:"config"`
		} `yaml:"vaults"`
	}
	require.NoError(t, yaml.Unmarshal(sniPayload, &dblessConfig))
	require.Len(t, dblessConfig.CACertificates, 2)
	// The vaults must reach the rendered payload with unique IDs: the converter derives each
	// entity ID from the vault prefix.
	require.Len(t, dblessConfig.Vaults, 2)
	require.Equal(t, "env-a", dblessConfig.Vaults[0].Prefix)
	require.Equal(t, "env", dblessConfig.Vaults[0].Name)
	require.Equal(t, map[string]any{"prefix": "MY_SECRET_"}, dblessConfig.Vaults[0].Config)
	require.NotEmpty(t, dblessConfig.Vaults[0].ID)
	require.NotEqual(t, dblessConfig.Vaults[0].ID, dblessConfig.Vaults[1].ID)
	tags := make([]string, 0, 2)
	for i, caCert := range dblessConfig.CACertificates {
		require.NotEmpty(t, caCert.ID, "ca_certificates[%d] has no ID", i)
		if i > 0 {
			require.NotEqual(t, dblessConfig.CACertificates[0].ID, caCert.ID, "duplicate CA certificate ID")
		}
		tags = append(tags, caCert.Tags...)
	}
	require.Contains(t, tags, "ai-gateway-name:ca-cert-a")
	require.Contains(t, tags, "ai-gateway-name:ca-cert-b")

	// Rendering must not fail even once a dangling reference is introduced by the
	// next slice - pinned here with a target that references a provider this test never
	// creates. The converter no longer fails the render for it: the target's model is left
	// without a convertible target, so it is dropped from the output and reported as an
	// ERROR-severity issue, while the rest of the document still renders.
	doc.Models[0].TargetModels = []aigw.TargetModel{{Name: "t", Provider: "does-not-exist"}}
	payload, issues, err := RenderDBLessYAML(doc, convert.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, payload)
	require.NotEmpty(t, issues)
	// The dropped target is reported with a warning issue first; the model left without a
	// convertible target follows as an ERROR issue.
	var modelErr *convert.ConversionIssue
	for i, issue := range issues {
		if issue.Severity == convert.Error {
			modelErr = &issues[i]
			break
		}
	}
	require.NotNil(t, modelErr, "expected an ERROR issue for the unconvertible model, got %v", issues)
	require.Equal(t, "model-a", modelErr.Source.Name)
}

// TestBuildDocument_NoModels covers the empty case: an OnPremAIGateway with no AIGatewayModels
// yet must not error.
func TestBuildDocument_NoModels(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)
	require.Empty(t, statuses)
	require.Empty(t, doc.Models)
	require.Empty(t, doc.ModelProviders)
	require.Empty(t, doc.Policies)
	require.Empty(t, doc.ConsumerGroups)
	require.Empty(t, doc.AuthStrategies)
	require.Empty(t, doc.Consumers)
	require.Empty(t, doc.CACertificates)
	require.Empty(t, doc.Certificates)
	require.Empty(t, doc.SNIs)
	require.Empty(t, doc.CustomPolicies)
}

// TestBuildDocument_PerEntityFailure covers the continue-on-error behaviour: a single broken
// entity is excluded from the document and reported with its conversion error, while the
// remaining entities still convert.
func TestBuildDocument_PerEntityFailure(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A model without spec.apiSpec fails conversion deterministically.
	broken := aiGatewayModelFixture("model-broken")
	broken.Spec.APISpec = aiconfigurationv1alpha1.AIGatewayModelAPISpec{}
	model := aiGatewayModelFixture("model-a")
	// The same for a CA certificate: proves the per-kind failure isolation is not
	// model-specific.
	brokenCACert := aiGatewayCACertificateFixture("ca-cert-broken", "")
	brokenCACert.Spec.APISpec = aiconfigurationv1alpha1.AIGatewayCACertificateAPISpec{}

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, broken, model, brokenCACert)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Models, 1)
	require.Equal(t, "model-a", doc.Models[0].Name)
	require.Empty(t, doc.CACertificates)

	require.Len(t, statuses, 3)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			switch name := s.Obj.GetName(); name {
			case "model-broken", "ca-cert-broken":
				require.Contains(t, s.Err.Error(), "spec.apiSpec is required")
			default:
				t.Errorf("unexpected failed entity %s: %v", name, s.Err)
			}
		}
	}
	require.Equal(t, 2, failed)
}

// TestBuildDocument_CACertificateDuplicateContent covers the duplicate-PEM rule: the converter
// derives the rendered CA certificate entity ID from the cert content, so two CRs sharing one
// PEM would render two entries with the same ID and fail the whole push. The first CR
// (namespace/name order) wins; the duplicate is reported per-entity and excluded.
func TestBuildDocument_CACertificateDuplicateContent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	sharedPEM := genCACertPEM(t)
	caCertA := aiGatewayCACertificateFixture("ca-cert-a", sharedPEM)
	caCertB := aiGatewayCACertificateFixture("ca-cert-b", sharedPEM)

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, caCertA, caCertB)
	// BuildDocument lists every configuration-entity kind, so every kind's index is needed.
	for _, opts := range [][]index.Option{
		index.OptionsForAIGatewayModel(),
		index.OptionsForAIGatewayModelProvider(),
		index.OptionsForAIGatewayPolicy(),
		index.OptionsForAIGatewayConsumerGroup(),
		index.OptionsForAIGatewayConsumer(),
		index.OptionsForAIGatewayConsumerCredential(),
		index.OptionsForAIGatewayAuthStrategy(),
		index.OptionsForAIGatewayCACertificate(),
		index.OptionsForAIGatewayCertificate(),
		index.OptionsForAIGatewaySNI(),
		index.OptionsForAIGatewayCustomPolicy(),
		index.OptionsForAIGatewayAgent(),
		index.OptionsForAIGatewayMCPServer(),
		index.OptionsForAIGatewayVault(),
	} {
		for _, opt := range opts {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.CACertificates, 1)
	require.Equal(t, "ca-cert-a", doc.CACertificates[0].Name)

	require.Len(t, statuses, 2)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			require.Equal(t, "ca-cert-b", s.Obj.GetName())
			require.ErrorContains(t, s.Err, "duplicate CA certificate content")
			require.ErrorContains(t, s.Err, "ca-cert-a")
		}
	}
	require.Equal(t, 1, failed)
}

// TestBuildDocument_VaultDuplicatePrefix mirrors TestBuildDocument_CACertificateDuplicateContent:
// the converter derives the rendered vault entity ID from the vault prefix, so two CRs sharing
// one prefix would render two entries with the same ID and fail the whole push. The first CR
// (namespace/name order) wins; the duplicate is reported per-entity and excluded.
func TestBuildDocument_VaultDuplicatePrefix(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	vaultA := aiGatewayVaultFixture("vault-a", "env")
	vaultB := aiGatewayVaultFixture("vault-b", "env")

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, vaultA, vaultB)
	// BuildDocument lists every configuration-entity kind, so every kind's index is needed.
	for _, opts := range [][]index.Option{
		index.OptionsForAIGatewayModel(),
		index.OptionsForAIGatewayModelProvider(),
		index.OptionsForAIGatewayPolicy(),
		index.OptionsForAIGatewayConsumerGroup(),
		index.OptionsForAIGatewayConsumer(),
		index.OptionsForAIGatewayConsumerCredential(),
		index.OptionsForAIGatewayAuthStrategy(),
		index.OptionsForAIGatewayCACertificate(),
		index.OptionsForAIGatewayCertificate(),
		index.OptionsForAIGatewaySNI(),
		index.OptionsForAIGatewayCustomPolicy(),
		index.OptionsForAIGatewayAgent(),
		index.OptionsForAIGatewayMCPServer(),
		index.OptionsForAIGatewayVault(),
	} {
		for _, opt := range opts {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Vaults, 1)
	require.Equal(t, "env", doc.Vaults[0].Name)

	require.Len(t, statuses, 2)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			require.Equal(t, "vault-b", s.Obj.GetName())
			require.ErrorContains(t, s.Err, `duplicate vault prefix "env"`)
			require.ErrorContains(t, s.Err, "vault-a")
		}
	}
	require.Equal(t, 1, failed)
}

// TestBuildDocument_SNIFailsWhenReferencedCertificateFails covers the cross-entity dependency:
// a certificate that fails its own translation is excluded from the document, and the SNI
// referencing it must fail too (instead of reporting success while the converter drops it from
// the pushed payload for the dangling reference).
func TestBuildDocument_SNIFailsWhenReferencedCertificateFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A certificate whose cert comes from a Secret that does not exist fails its own
	// translation; the healthy pair proves the SNI failure stays per-entity.
	brokenCert := aiGatewayCertificateFixture("cert-broken")
	brokenCert.Spec.APISpec.Cert = aiconfigurationv1alpha1.SensitiveDataSource{
		Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "missing-secret", Key: "tls.crt"},
	}
	healthyCert := aiGatewayCertificateFixture("cert-healthy")
	sniBroken := aiGatewaySNIFixture("sni-broken", "cert-broken")
	sniHealthy := aiGatewaySNIFixture("sni-healthy", "cert-healthy")

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, brokenCert, healthyCert, sniBroken, sniHealthy)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Certificates, 1)
	require.Equal(t, "cert-healthy", doc.Certificates[0].Name)
	require.Len(t, doc.SNIs, 1)
	require.Equal(t, "sni-healthy", doc.SNIs[0].Name)
	require.Equal(t, "cert-healthy", doc.SNIs[0].Certificate)

	require.Len(t, statuses, 4)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			switch name := s.Obj.GetName(); name {
			case "cert-broken":
				require.Contains(t, s.Err.Error(), "missing-secret")
			case "sni-broken":
				require.Contains(t, s.Err.Error(), "its own translation failed")
			default:
				t.Errorf("unexpected failed entity %s: %v", name, s.Err)
			}
		}
	}
	require.Equal(t, 2, failed)
}

// TestBuildDocument_CrossNamespaceEntityRejected covers the same-namespace rule: an entity
// referencing the OnPremAIGateway from another namespace is rejected with a per-entity error
// (surfacing as the entity's Programmed condition) instead of failing later on its Secrets
// being invisible to the gateway-namespace scoped cache.
func TestBuildDocument_CrossNamespaceEntityRejected(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// An AuthStrategy in another namespace explicitly targeting the gateway in "default":
	// the field index matches it, so the translation must reject it explicitly.
	crossNamespace := aiGatewayAuthStrategyFixture("cross-ns")
	crossNamespace.Namespace = "other"
	crossNamespace.Spec.AIGatewayRef.NamespacedRef = &commonv1alpha1.NamespacedRef{
		Namespace: new("default"),
		Name:      "gw",
	}
	sameNamespace := aiGatewayAuthStrategyFixture("same-ns")

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, crossNamespace, sameNamespace)
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Empty(t, doc.Models)
	require.Empty(t, doc.ModelProviders)
	require.Empty(t, doc.Policies)
	require.Empty(t, doc.ConsumerGroups)
	require.Empty(t, doc.Consumers)
	require.Empty(t, doc.CACertificates)
	require.Empty(t, doc.Certificates)
	require.Empty(t, doc.SNIs)
	require.Empty(t, doc.CustomPolicies)
	require.Empty(t, doc.Vaults)
	require.Len(t, doc.AuthStrategies, 1)
	require.Equal(t, "same-ns", doc.AuthStrategies[0].Name)

	require.Len(t, statuses, 2)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			require.Equal(t, "cross-ns", s.Obj.GetName())
			require.Contains(t, s.Err.Error(),
				"cross-namespace reference to OnPremAIGateway default/gw is not supported on-prem")
		}
	}
	require.Equal(t, 1, failed)
}

// TestBuildDocument_CredentialChangeRerender pins the credential -> document flow: the
// rendered consumer embeds its credentials' secret-resolved api keys, so a change to the
// backing Secret changes the rendered document (and, in production, the payload hash that
// drives the push loop).
func TestBuildDocument_CredentialChangeRerender(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	consumer := aiGatewayConsumerFixture("consumer-a")
	credential := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Name: "cred-a", Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "consumer-a"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				Name:        "cred-1",
				DisplayName: "Cred 1",
				Type:        "api-key",
				APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
					Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
						Name: "consumer-api-key",
						Key:  "key",
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		Name: "consumer-api-key", Namespace: "default",
		Data: map[string][]byte{"key": []byte("s3cr3t")},
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, consumer, credential, secret)
	// BuildDocument lists every configuration-entity kind, so every kind's index is needed.
	for _, opts := range [][]index.Option{
		index.OptionsForAIGatewayModel(),
		index.OptionsForAIGatewayModelProvider(),
		index.OptionsForAIGatewayPolicy(),
		index.OptionsForAIGatewayConsumerGroup(),
		index.OptionsForAIGatewayConsumer(),
		index.OptionsForAIGatewayConsumerCredential(),
		index.OptionsForAIGatewayAuthStrategy(),
		index.OptionsForAIGatewayCACertificate(),
		index.OptionsForAIGatewayCertificate(),
		index.OptionsForAIGatewaySNI(),
		index.OptionsForAIGatewayCustomPolicy(),
		index.OptionsForAIGatewayAgent(),
		index.OptionsForAIGatewayMCPServer(),
		index.OptionsForAIGatewayVault(),
	} {
		for _, opt := range opts {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.NoError(t, statuses[0].Err)
	require.Len(t, doc.Consumers, 1)
	require.Len(t, doc.Consumers[0].Credentials, 1)
	require.Equal(t, "s3cr3t", doc.Consumers[0].Credentials[0].APIKey)

	// Rotate the Secret's key value: the re-rendered document must reflect it.
	secret.Data["key"] = []byte("n3w-s3cr3t")
	require.NoError(t, cl.Update(t.Context(), secret))

	doc, statuses, err = BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.NoError(t, statuses[0].Err)
	require.Len(t, doc.Consumers, 1)
	require.Equal(t, "n3w-s3cr3t", doc.Consumers[0].Credentials[0].APIKey)
}

// newTestBuilder returns a fake client builder with every AI Gateway configuration entity
// kind's OnOnPremAIGatewayRef field index registered, ready for the test objects.
func newTestBuilder(scheme *runtime.Scheme, objs ...client.Object) *fake.ClientBuilder {
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
	for _, opts := range [][]index.Option{
		index.OptionsForAIGatewayModel(),
		index.OptionsForAIGatewayModelProvider(),
		index.OptionsForAIGatewayPolicy(),
		index.OptionsForAIGatewayConsumerGroup(),
		index.OptionsForAIGatewayAuthStrategy(),
		index.OptionsForAIGatewayConsumerCredential(),
		index.OptionsForAIGatewayConsumer(),
		index.OptionsForAIGatewayCACertificate(),
		index.OptionsForAIGatewayCertificate(),
		index.OptionsForAIGatewaySNI(),
		index.OptionsForAIGatewayCustomPolicy(),
		index.OptionsForAIGatewayAgent(),
		index.OptionsForAIGatewayMCPServer(),
		index.OptionsForAIGatewayVault(),
	} {
		for _, opt := range opts {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
	}
	return builder
}

// findStatusByName returns the EntityStatus of the entity named name, requiring it to exist.
func findStatusByName(t *testing.T, statuses []EntityStatus, name string) EntityStatus {
	t.Helper()
	for _, s := range statuses {
		if s.Obj.GetName() == name {
			return s
		}
	}
	t.Fatalf("no EntityStatus for entity %q", name)
	return EntityStatus{}
}

// TestBuildDocument_ModelFailsWhenReferencedProviderFails pins the translated-reference check
// for model targets: a model targeting an AIGatewayModelProvider that failed its own
// translation must be excluded with a per-entity error, instead of reporting success while
// the converter drops it from the pushed payload.
func TestBuildDocument_ModelFailsWhenReferencedProviderFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A provider with a cross-namespace secretRef fails its own translation while keeping its
	// entity name intact; the healthy pair proves the model failure stays per-entity.
	brokenProvider := aiGatewayModelProviderFixture("provider-broken")
	brokenProvider.Spec.APISpec.Openai.Config.Auth.Headers = []aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasicHeaders{{
		Name: "Authorization",
		Value: aiconfigurationv1alpha1.SensitiveDataSource{
			Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
			SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "creds", Key: "key", Namespace: new("other-ns")},
		},
	}}
	healthyProvider := aiGatewayModelProviderFixture("provider-healthy")

	withTarget := func(m *aiconfigurationv1alpha1.AIGatewayModel, provider string) *aiconfigurationv1alpha1.AIGatewayModel {
		m.Spec.APISpec.Model.Targets = []aiconfigurationv1alpha1.AIGatewayTarget{{
			Name:     "t",
			Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{Name: provider},
			Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
				Type:   aiconfigurationv1alpha1.AIGatewayTargetConfigTypeOpenai,
				Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1"},
			},
		}}
		return m
	}
	modelBroken := withTarget(aiGatewayModelFixture("model-broken"), "provider-broken")
	modelHealthy := withTarget(aiGatewayModelFixture("model-healthy"), "provider-healthy")

	cl := newTestBuilder(scheme, gw, brokenProvider, healthyProvider, modelBroken, modelHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.ModelProviders, 1)
	require.Equal(t, "provider-healthy", doc.ModelProviders[0].Name)
	require.Len(t, doc.Models, 1)
	require.Equal(t, "model-healthy", doc.Models[0].Name)
	require.Equal(t, "provider-healthy", doc.Models[0].TargetModels[0].Provider)

	require.Len(t, statuses, 4)
	require.Contains(t, findStatusByName(t, statuses, "provider-broken").Err.Error(), "cross-namespace secretRef")
	require.Contains(t, findStatusByName(t, statuses, "model-broken").Err.Error(),
		`referenced AIGatewayModelProvider "provider-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "model-healthy").Err)
}

// TestBuildDocument_ConsumerFailsWhenReferencedPolicyFails pins the translated-reference check
// for consumer policy references: a consumer referencing an AIGatewayPolicy that failed its
// own translation must be excluded with a per-entity error.
func TestBuildDocument_ConsumerFailsWhenReferencedPolicyFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A policy with a cross-namespace secretRef fails its own translation while keeping its
	// entity name intact.
	brokenPolicy := aiGatewayPolicyFixture("policy-broken")
	brokenPolicy.Spec.APISpec.Config = aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
		Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "creds", Key: "key", Namespace: new("other-ns")},
	}
	healthyPolicy := aiGatewayPolicyFixture("policy-healthy")

	consumerBroken := aiGatewayConsumerFixture("consumer-broken")
	consumerBroken.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-broken"}}
	consumerHealthy := aiGatewayConsumerFixture("consumer-healthy")
	consumerHealthy.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-healthy"}}

	cl := newTestBuilder(scheme, gw, brokenPolicy, healthyPolicy, consumerBroken, consumerHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Policies, 1)
	require.Equal(t, "policy-healthy", doc.Policies[0].Name)
	require.Len(t, doc.Consumers, 1)
	require.Equal(t, "consumer-healthy", doc.Consumers[0].Name)
	require.Equal(t, []string{"policy-healthy"}, doc.Consumers[0].Policies)

	require.Len(t, statuses, 4)
	require.Contains(t, findStatusByName(t, statuses, "policy-broken").Err.Error(), "cross-namespace secretRef")
	require.Contains(t, findStatusByName(t, statuses, "consumer-broken").Err.Error(),
		`referenced AIGatewayPolicy "policy-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "consumer-healthy").Err)
}

// TestBuildDocument_ConsumerGroupFailsWhenReferencedPolicyFails pins the translated-reference
// check for consumer-group policy references: a consumer group referencing an AIGatewayPolicy
// that failed its own translation must be excluded with a per-entity error.
func TestBuildDocument_ConsumerGroupFailsWhenReferencedPolicyFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A policy with a cross-namespace secretRef fails its own translation while keeping its
	// entity name intact.
	brokenPolicy := aiGatewayPolicyFixture("policy-broken")
	brokenPolicy.Spec.APISpec.Config = aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
		Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "creds", Key: "key", Namespace: new("other-ns")},
	}
	healthyPolicy := aiGatewayPolicyFixture("policy-healthy")

	groupBroken := aiGatewayConsumerGroupFixture("group-broken")
	groupBroken.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-broken"}}
	groupHealthy := aiGatewayConsumerGroupFixture("group-healthy")
	groupHealthy.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-healthy"}}

	cl := newTestBuilder(scheme, gw, brokenPolicy, healthyPolicy, groupBroken, groupHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Policies, 1)
	require.Equal(t, "policy-healthy", doc.Policies[0].Name)
	require.Len(t, doc.ConsumerGroups, 1)
	require.Equal(t, "group-healthy", doc.ConsumerGroups[0].Name)
	require.Equal(t, []string{"policy-healthy"}, doc.ConsumerGroups[0].Policies)

	require.Len(t, statuses, 4)
	require.Contains(t, findStatusByName(t, statuses, "policy-broken").Err.Error(), "cross-namespace secretRef")
	require.Contains(t, findStatusByName(t, statuses, "group-broken").Err.Error(),
		`referenced AIGatewayPolicy "policy-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "group-healthy").Err)
}

// TestBuildDocument_AgentFailsWhenReferencedAuthStrategyFails pins the translated-reference
// check for agent auth-strategy references: an agent referencing an AIGatewayAuthStrategy
// that failed its own translation must be excluded with a per-entity error.
func TestBuildDocument_AgentFailsWhenReferencedAuthStrategyFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// An auth strategy with a cross-namespace secretRef fails its own translation while
	// keeping its entity name intact.
	brokenAuthStrategy := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		Name: "auth-strategy-broken", Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeOpenIDConnect,
					OpenIDConnect: &aiconfigurationv1alpha1.AIGatewayAuthStrategyOpenIDConnect{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier("auth-strategy-broken"),
						DisplayName: "auth-strategy-broken",
						Config: aiconfigurationv1alpha1.AIGWOpenIDConnectGeneratedConfig{
							ClientSecret: []aiconfigurationv1alpha1.SensitiveDataSource{{
								Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "oidc-secret", Key: "clientSecret", Namespace: new("other-ns")},
							}},
						},
					},
				},
			},
		},
	}
	healthyAuthStrategy := aiGatewayAuthStrategyFixture("auth-strategy-healthy")
	healthyPolicy := aiGatewayPolicyFixture("policy-healthy")
	healthyGroup := aiGatewayConsumerGroupFixture("group-healthy")

	agentBroken := aiGatewayAgentFixture("agent-broken")
	agentBroken.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-healthy"}}
	agentBroken.Spec.APISpec.Access.Acls.Allow.Allow = []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: "group-healthy"}}
	agentBroken.Spec.APISpec.Access.AuthStrategies = []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{{Name: "auth-strategy-broken"}}
	agentHealthy := aiGatewayAgentFixture("agent-healthy")
	agentHealthy.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-healthy"}}
	agentHealthy.Spec.APISpec.Access.Acls.Allow.Allow = []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: "group-healthy"}}
	agentHealthy.Spec.APISpec.Access.AuthStrategies = []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{{Name: "auth-strategy-healthy"}}

	cl := newTestBuilder(scheme, gw, brokenAuthStrategy, healthyAuthStrategy, healthyPolicy, healthyGroup, agentBroken, agentHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.AuthStrategies, 1)
	require.Equal(t, "auth-strategy-healthy", doc.AuthStrategies[0].Name)
	require.Len(t, doc.Agents, 1)
	require.Equal(t, "agent-healthy", doc.Agents[0].Name)

	require.Len(t, statuses, 6)
	require.Contains(t, findStatusByName(t, statuses, "auth-strategy-broken").Err.Error(), "cross-namespace secretRef")
	require.Contains(t, findStatusByName(t, statuses, "agent-broken").Err.Error(),
		`referenced AIGatewayAuthStrategy "auth-strategy-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "agent-healthy").Err)
}

// TestBuildDocument_MCPServerFailsWhenReferencedConsumerGroupFails pins the translated-
// reference check for the MCP server ACL surface (which also covers DefaultToolACLs and
// per-tool ACLs through the same helper): an MCP server whose access.acls.allow references
// an AIGatewayConsumerGroup that failed its own translation must be excluded with a
// per-entity error.
func TestBuildDocument_MCPServerFailsWhenReferencedConsumerGroupFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A consumer group referencing a nonexistent policy fails its own translation while
	// keeping its entity name intact.
	brokenGroup := aiGatewayConsumerGroupFixture("group-broken")
	brokenGroup.Spec.APISpec.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "does-not-exist"}}
	healthyGroup := aiGatewayConsumerGroupFixture("group-healthy")
	healthyPolicy := aiGatewayPolicyFixture("policy-healthy")

	withAccess := func(s *aiconfigurationv1alpha1.AIGatewayMCPServer, group string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
		// The fixture auto-references policy-<last char of name>; point it at the healthy
		// policy instead so only the consumer-group reference varies.
		s.Spec.APISpec.PassthroughListener.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-healthy"}}
		s.Spec.APISpec.PassthroughListener.Access = &aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListenerAccess{
			AclAttributeType: aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListenerAccessTypeConsumer,
			Consumer: &aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumer{
				Acls: aiconfigurationv1alpha1.AIGatewayMCPACLs{
					Allow: []aiconfigurationv1alpha1.AIGatewayMCPACLRef{{Name: group}},
				},
			},
		}
		return s
	}
	mcpBroken := withAccess(aiGatewayMCPServerFixture("mcp-broken"), "group-broken")
	mcpHealthy := withAccess(aiGatewayMCPServerFixture("mcp-healthy"), "group-healthy")

	cl := newTestBuilder(scheme, gw, brokenGroup, healthyGroup, healthyPolicy, mcpBroken, mcpHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.ConsumerGroups, 1)
	require.Equal(t, "group-healthy", doc.ConsumerGroups[0].Name)
	require.Len(t, doc.MCPServers, 1)
	require.Equal(t, "mcp-healthy", doc.MCPServers[0].Name)
	require.Equal(t, []string{"group-healthy"}, doc.MCPServers[0].Access.ACLs.Allow)

	require.Len(t, statuses, 5)
	require.Contains(t, findStatusByName(t, statuses, "group-broken").Err.Error(), "does-not-exist")
	require.Contains(t, findStatusByName(t, statuses, "mcp-broken").Err.Error(),
		`referenced AIGatewayConsumerGroup "group-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "mcp-healthy").Err)
}

// aiGatewayMCPServerConversionOnlyFixture returns a healthy conversion-only MCP server (a
// valid same-kind source for listeners), and aiGatewayMCPServerListenerFixture a listener
// exposing the given sources' tools.
func aiGatewayMCPServerConversionOnlyFixture(name string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	return &aiconfigurationv1alpha1.AIGatewayMCPServer{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
					Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeConversionOnly,
					ConversionOnly: &aiconfigurationv1alpha1.AIGatewayMCPServerConversionOnly{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
					},
				},
			},
		},
	}
}

func aiGatewayMCPServerListenerFixture(name string, sources ...string) *aiconfigurationv1alpha1.AIGatewayMCPServer {
	mcp := &aiconfigurationv1alpha1.AIGatewayMCPServer{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
					Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypeListener,
					Listener: &aiconfigurationv1alpha1.AIGatewayMCPServerListener{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
						DisplayName: name,
						Config: aiconfigurationv1alpha1.AIGatewayMCPServerListenerConfig{
							Route: aiconfigurationv1alpha1.AIGatewayMCPServerRouteWithMatcher{
								Paths: []string{"/mcp/" + name},
							},
						},
					},
				},
			},
		},
	}
	for _, src := range sources {
		mcp.Spec.APISpec.Listener.Sources = append(mcp.Spec.APISpec.Listener.Sources,
			aiconfigurationv1alpha1.AIGatewayMCPServerRef{Name: src})
	}
	return mcp
}

// TestBuildDocument_MCPServerListenerFailsWhenSourceFails pins the same-kind reference check
// for MCP server sources: a listener sourcing an AIGatewayMCPServer that failed its own
// translation must be dropped from the document with its (previously successful) status
// patched, including the cascade where another listener sources the dropped one.
func TestBuildDocument_MCPServerListenerFailsWhenSourceFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// The broken source fails its own translation (a policy ref to a nonexistent CR) while
	// keeping its entity name intact; the listener sourcing it and the listener sourcing
	// that listener are both dropped (fixpoint cascade), while the listener sourcing
	// the healthy source survives.
	srcBroken := aiGatewayMCPServerConversionOnlyFixture("mcp-src-broken")
	srcBroken.Spec.APISpec.ConversionOnly.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "does-not-exist"}}
	srcHealthy := aiGatewayMCPServerConversionOnlyFixture("mcp-src-healthy")
	listenerBroken := aiGatewayMCPServerListenerFixture("mcp-listener-broken", "mcp-src-broken")
	listenerCascade := aiGatewayMCPServerListenerFixture("mcp-listener-cascade", "mcp-listener-broken")
	listenerHealthy := aiGatewayMCPServerListenerFixture("mcp-listener-healthy", "mcp-src-healthy")

	cl := newTestBuilder(scheme, gw, srcBroken, srcHealthy, listenerBroken, listenerCascade, listenerHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.MCPServers, 2)
	require.Equal(t, "mcp-listener-healthy", doc.MCPServers[0].Name)
	require.Equal(t, []string{"mcp-src-healthy"}, doc.MCPServers[0].Config.Sources)
	require.Equal(t, "mcp-src-healthy", doc.MCPServers[1].Name)

	require.Len(t, statuses, 5)
	require.Contains(t, findStatusByName(t, statuses, "mcp-src-broken").Err.Error(), "does-not-exist")
	require.Contains(t, findStatusByName(t, statuses, "mcp-listener-broken").Err.Error(),
		`referenced AIGatewayMCPServer "mcp-src-broken" was excluded from the document`)
	require.Contains(t, findStatusByName(t, statuses, "mcp-listener-cascade").Err.Error(),
		`referenced AIGatewayMCPServer "mcp-listener-broken" was excluded from the document: its own translation failed, or it references an excluded server`)
	require.NoError(t, findStatusByName(t, statuses, "mcp-src-healthy").Err)
	require.NoError(t, findStatusByName(t, statuses, "mcp-listener-healthy").Err)
}

// TestBuildDocument_MCPServerSourceSortedLater is the regression guard for the same-kind
// fixpoint: a listener may source an MCP server sorted later in the kind, which a naive
// translate-time check would reject. Both must survive.
func TestBuildDocument_MCPServerSourceSortedLater(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// "mcp-a" sorts before its source "mcp-z".
	listener := aiGatewayMCPServerListenerFixture("mcp-a", "mcp-z")
	source := aiGatewayMCPServerConversionOnlyFixture("mcp-z")

	cl := newTestBuilder(scheme, gw, listener, source).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.MCPServers, 2)
	require.Equal(t, "mcp-a", doc.MCPServers[0].Name)
	require.Equal(t, []string{"mcp-z"}, doc.MCPServers[0].Config.Sources)
	require.Equal(t, "mcp-z", doc.MCPServers[1].Name)

	require.Len(t, statuses, 2)
	for _, s := range statuses {
		require.NoError(t, s.Err)
	}
}

// TestBuildDocument_MCPServerDuplicateEntityNameRejected pins the duplicate entity-name
// rejection: two AIGatewayMCPServer CRs sharing one spec name would render the same entity
// twice and break the name-keyed maps, so the second CR is rejected with a per-entity error.
func TestBuildDocument_MCPServerDuplicateEntityNameRejected(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// Two CRs with distinct metadata names but the same spec `name` render the same
	// entity name; "mcp-dup-a" sorts before "mcp-dup-b", so it wins.
	first := aiGatewayMCPServerConversionOnlyFixture("mcp-dup")
	first.Name = "mcp-dup-a"
	second := aiGatewayMCPServerConversionOnlyFixture("mcp-dup")
	second.Name = "mcp-dup-b"

	cl := newTestBuilder(scheme, gw, first, second).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.MCPServers, 1)
	require.Equal(t, "mcp-dup", doc.MCPServers[0].Name)

	require.Len(t, statuses, 2)
	require.Contains(t, findStatusByName(t, statuses, "mcp-dup-b").Err.Error(),
		`duplicate AIGatewayMCPServer entity name "mcp-dup"`)
	require.NoError(t, findStatusByName(t, statuses, "mcp-dup-a").Err)
}
