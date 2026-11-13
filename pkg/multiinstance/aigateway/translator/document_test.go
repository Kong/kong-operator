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
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
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

// withRoutedTarget gives the model a capability and a target on the
// "provider-shared" provider. Without both, the converter renders no route
// for the model, and no same-route alias conflict can occur.
func withRoutedTarget(m *aiconfigurationv1alpha1.AIGatewayModel) *aiconfigurationv1alpha1.AIGatewayModel {
	m.Spec.APISpec.Model.Capabilities = []string{"generate"}
	m.Spec.APISpec.Model.Targets = []aiconfigurationv1alpha1.AIGatewayTarget{{
		Name:     "t",
		Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{Name: "provider-shared"},
		Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
			Type:   aiconfigurationv1alpha1.AIGatewayTargetConfigTypeOpenai,
			Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1"},
		},
	}}
	return m
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

// TestRenderDBLessYAML_EmptyConfigGetsStubUpstream covers the stub entity: a Kong data
// plane does not report ready while its applied configuration is empty, so a gateway
// with no configuration entities gets an inert upstream instead of an empty payload.
func TestRenderDBLessYAML_EmptyConfigGetsStubUpstream(t *testing.T) {
	t.Parallel()

	payload, issues, err := RenderDBLessYAML(&aigw.Document{}, convert.Options{})
	require.NoError(t, err)
	require.Empty(t, issues)

	var content struct {
		FormatVersion string `yaml:"_format_version"`
		Upstreams     []struct {
			Name string `yaml:"name"`
		} `yaml:"upstreams"`
	}
	require.NoError(t, yaml.Unmarshal(payload, &content))
	require.NotEmpty(t, content.FormatVersion, "the rendered format version must be kept")
	require.Len(t, content.Upstreams, 1)
	require.Equal(t, stubUpstreamName, content.Upstreams[0].Name)

	// A payload that carries entities is not touched.
	withEntities := []byte("_format_version: \"3.0\"\n_transform: false\nservices:\n- name: svc\n")
	out, err := ensureNonEmptyConfig(withEntities)
	require.NoError(t, err)
	require.Equal(t, withEntities, out)

	// A payload that decodes to no mapping at all is stubbed too, rather than
	// failing on a write to a nil map. The format version is seeded, because
	// Kong rejects a payload that carries none.
	out, err = ensureNonEmptyConfig(nil)
	require.NoError(t, err)
	require.Equal(t, "_format_version: \"3.0\"\nupstreams:\n    - name: kong\n", string(out))
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

// TestBuildDocument_ModelDuplicateAlias covers two models sharing a selector
// alias on one route: the converter renders one ai_models entity per alias,
// named after it, and two models claiming one alias on one route group make
// identically scoped ai-proxy plugins whose backend selection the data plane
// decides internally. The earliest-created model keeps the alias (ties fall
// back to namespace/name order); the later claimant is excluded from the
// document and reported per-entity instead. Sharing an alias across DIFFERENT
// routes is not a conflict: the converter renders one shared ai_models row and
// both models stay.
func TestBuildDocument_ModelDuplicateAlias(t *testing.T) {
	t.Parallel()

	// buildClient assembles the fake client with the index every kind needs:
	// BuildDocument lists every configuration-entity kind. It always includes
	// the shared provider the alias fixtures' targets reference: a model
	// without a target renders no route, and the same-route conflicts under
	// test need rendered routes.
	buildClient := func(t *testing.T, objects ...client.Object) client.Client {
		t.Helper()
		scheme := runtime.NewScheme()
		require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
		require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

		// The fake tracker stamps resource versions into the objects it is
		// given, so parallel subtests must not share a fixture.
		objs := make([]client.Object, 0, len(objects))
		objs = append(objs, aiGatewayModelProviderFixture("provider-shared"))
		for _, obj := range objects {
			objs = append(objs, obj.DeepCopyObject().(client.Object))
		}

		builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
		for _, opt := range index.OptionsForAIGatewayEntities() {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
		return builder.Build()
	}

	// withAlias gives the model the shared alias on the shared route path, so
	// two of these models land on one route group and conflict.
	withAlias := func(name string, created metav1.Time) *aiconfigurationv1alpha1.AIGatewayModel {
		m := aiGatewayModelFixture(name)
		m.CreationTimestamp = created
		m.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
		m.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
			Values: []string{"shared-alias"},
		}
		return withRoutedTarget(m)
	}

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	dayOne := metav1.NewTime(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
	dayTwo := metav1.NewTime(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))

	t.Run("the earliest-created model keeps the alias", func(t *testing.T) {
		t.Parallel()

		// scoped sorts after plain, so name order would pick plain-model:
		// the creation-time winner proves the rule is not name-based.
		plain := withAlias("plain-model", dayTwo)
		scoped := withAlias("scoped-model", dayOne)

		cl := buildClient(t, gw.DeepCopy(), plain, scoped)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		require.Len(t, doc.Models, 1)
		require.Equal(t, "scoped-model", doc.Models[0].Name)

		require.Len(t, statuses, 3)
		conflicts := 0
		for _, s := range statuses {
			if s.Err != nil {
				conflicts++
				require.Equal(t, "plain-model", s.Obj.GetName())
				require.ErrorContains(t, s.Err, `duplicate model alias "shared-alias" on route "openai-chat" (paths /shared)`)
				require.ErrorContains(t, s.Err, `"scoped-model"`)
			}
		}
		require.Equal(t, 1, conflicts)
	})

	t.Run("ties fall back to namespace/name order", func(t *testing.T) {
		t.Parallel()

		// Equal creation timestamps (e.g. two models applied in the same
		// second): the entity names break the tie deterministically.
		plain := withAlias("plain-model", dayOne)
		scoped := withAlias("scoped-model", dayOne)

		cl := buildClient(t, gw.DeepCopy(), plain, scoped)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		require.Len(t, doc.Models, 1)
		require.Equal(t, "plain-model", doc.Models[0].Name)

		require.Len(t, statuses, 3)
		conflicts := 0
		for _, s := range statuses {
			if s.Err != nil {
				conflicts++
				require.Equal(t, "scoped-model", s.Obj.GetName())
				require.ErrorContains(t, s.Err, `duplicate model alias "shared-alias"`)
				require.ErrorContains(t, s.Err, `"plain-model"`)
			}
		}
		require.Equal(t, 1, conflicts)
	})

	t.Run("an excluded model does not exclude later claimants", func(t *testing.T) {
		t.Parallel()

		// middle loses "x" to oldest, so its claim on "y" must not exclude newest.
		oldest := withRoutedTarget(aiGatewayModelFixture("oldest"))
		oldest.CreationTimestamp = dayOne
		oldest.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
		oldest.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
			Values: []string{"x"},
		}
		middle := withRoutedTarget(aiGatewayModelFixture("middle"))
		middle.CreationTimestamp = dayTwo
		middle.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
		middle.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
			Values: []string{"x", "y"},
		}
		newest := withRoutedTarget(aiGatewayModelFixture("newest"))
		newest.CreationTimestamp = metav1.NewTime(dayTwo.Add(24 * time.Hour))
		newest.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
		newest.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
			Values: []string{"y"},
		}

		cl := buildClient(t, gw.DeepCopy(), oldest, middle, newest)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		names := make([]string, 0, len(doc.Models))
		for _, m := range doc.Models {
			names = append(names, m.Name)
		}
		require.ElementsMatch(t, []string{"oldest", "newest"}, names)

		require.Len(t, statuses, 4)
		for _, s := range statuses {
			if s.Obj.GetName() != "middle" {
				require.NoError(t, s.Err)
				continue
			}
			require.ErrorContains(t, s.Err, `duplicate model alias "x"`)
			require.ErrorContains(t, s.Err, `"oldest"`)
		}
	})

	t.Run("a disabled model claims no alias", func(t *testing.T) {
		t.Parallel()

		// The converter renders nothing for a disabled model, so the older
		// disabled one must not exclude the newer enabled one.
		disabled := withAlias("disabled-model", dayOne)
		disabled.Spec.APISpec.Model.Enabled = "Disabled"
		enabled := withAlias("enabled-model", dayTwo)

		cl := buildClient(t, gw.DeepCopy(), disabled, enabled)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		require.Len(t, doc.Models, 2)
		require.Len(t, statuses, 3)
		for _, s := range statuses {
			require.NoError(t, s.Err, s.Obj.GetName())
		}
	})

	t.Run("a model on many routes keeps the alias on each route", func(t *testing.T) {
		t.Parallel()

		// Each capability renders its own route, so wide conflicts with each
		// later model on a different route. All of them must lose.
		wide := withAlias("wide", dayOne)
		wide.Spec.APISpec.Model.Capabilities = []string{"generate", "agentic", "embeddings", "image"}
		objs := []client.Object{gw.DeepCopy(), wide}
		for i, capability := range []string{"generate", "agentic", "embeddings", "image"} {
			m := withAlias("narrow-"+strings.ReplaceAll(capability, "/", "-"), metav1.NewTime(dayTwo.Add(time.Duration(i)*time.Hour)))
			m.Spec.APISpec.Model.Capabilities = []string{capability}
			objs = append(objs, m)
		}

		cl := buildClient(t, objs...)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		require.Len(t, doc.Models, 1)
		require.Equal(t, "wide", doc.Models[0].Name)
		_, issues, err := RenderDBLessYAML(doc, convert.Options{})
		require.NoError(t, err)
		require.Empty(t, collectAliasConflicts(issues))

		require.Len(t, statuses, 6)
		for _, s := range statuses {
			if !strings.HasPrefix(s.Obj.GetName(), "narrow-") {
				require.NoError(t, s.Err, s.Obj.GetName())
				continue
			}
			require.ErrorContains(t, s.Err, `duplicate model alias "shared-alias"`)
			require.ErrorContains(t, s.Err, `AIGatewayModel "wide"`)
		}
	})

	t.Run("the same alias on different routes is not a conflict", func(t *testing.T) {
		t.Parallel()

		// Distinct route paths put the models on different route groups: the
		// converter renders one shared ai_models row for the alias and both
		// models keep their own route and plugin, so both stay Programmed.
		left := withAlias("left-model", dayOne)
		left.Spec.APISpec.Model.Config.Route.Paths = []string{"/left"}
		right := withAlias("right-model", dayTwo)
		right.Spec.APISpec.Model.Config.Route.Paths = []string{"/right"}

		cl := buildClient(t, gw.DeepCopy(), left, right)
		doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
		require.NoError(t, err)

		names := make([]string, 0, len(doc.Models))
		for _, m := range doc.Models {
			names = append(names, m.Name)
		}
		require.ElementsMatch(t, []string{"left-model", "right-model"}, names)
		require.Len(t, statuses, 3)
		for _, s := range statuses {
			require.NoError(t, s.Err, s.Obj.GetName())
		}
	})
}

// TestBuildDocument_ModelAliasCollisionVariants covers the remaining collision
// shapes of the converter-derived alias rule: a no-selector model's entity name
// is itself an alias, and each value of a multi-value selector is claimed
// separately. All models share one route path, so a wrong alias rule makes a
// same-route conflict and fails the test.
func TestBuildDocument_ModelAliasCollisionVariants(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// No selector: the model's entity name is its alias. model-c's selector
	// value collides with it.
	modelA := withRoutedTarget(aiGatewayModelFixture("model-a"))
	modelA.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelC := withRoutedTarget(aiGatewayModelFixture("model-c"))
	modelC.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelC.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"model-a", "model-c-alias"},
	}
	// Multi-value selectors: model-d claims two aliases, model-e shares the
	// second one (its first alias is unique).
	modelD := withRoutedTarget(aiGatewayModelFixture("model-d"))
	modelD.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelD.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"d-alias-1", "d-alias-2"},
	}
	modelE := withRoutedTarget(aiGatewayModelFixture("model-e"))
	modelE.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelE.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"e-alias", "d-alias-2"},
	}
	// Duplicate values within one model collapse to their first occurrence
	// (mirroring the converter's extractModelAliases): no conflict, one entity.
	modelF := withRoutedTarget(aiGatewayModelFixture("model-f"))
	modelF.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelF.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"f-alias", "f-alias"},
	}
	// With a selector, the entity name is not an alias: model-h's value equal
	// to model-g's name is no conflict.
	modelG := withRoutedTarget(aiGatewayModelFixture("model-g"))
	modelG.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelG.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"g-alias"},
	}
	modelH := withRoutedTarget(aiGatewayModelFixture("model-h"))
	modelH.Spec.APISpec.Model.Config.Route.Paths = []string{"/shared"}
	modelH.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
		Values: []string{"model-g"},
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, aiGatewayModelProviderFixture("provider-shared"),
			modelA, modelC, modelD, modelE, modelF, modelG, modelH)
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	// Equal (zero) creation timestamps: the tie-break by name decides. model-c
	// and model-e conflict; the rest survive.
	names := make([]string, 0, len(doc.Models))
	for _, m := range doc.Models {
		names = append(names, m.Name)
	}
	require.ElementsMatch(t, []string{"model-a", "model-d", "model-f", "model-g", "model-h"}, names)

	wantConflicts := map[string][]string{
		"model-c": {`duplicate model alias "model-a"`, `AIGatewayModel "model-a"`},
		"model-e": {`duplicate model alias "d-alias-2"`, `AIGatewayModel "model-d"`},
	}
	require.Len(t, statuses, 8)
	for _, s := range statuses {
		want, conflict := wantConflicts[s.Obj.GetName()]
		if !conflict {
			require.NoError(t, s.Err, s.Obj.GetName())
			continue
		}
		for _, w := range want {
			require.ErrorContains(t, s.Err, w)
		}
	}
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

// newTranslatorTestClient builds a fake client with a scheme holding the OnPremAIGateway,
// the configuration entities and the KongReferenceGrants a cross-namespace reference is
// checked against, with every AI Gateway configuration index registered, so that
// BuildDocument can list entities per gateway.
func newTranslatorTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	// The fake tracker stamps resource versions into the objects it is given,
	// so parallel subtests must not share a fixture.
	copies := make([]client.Object, 0, len(objs))
	for _, obj := range objs {
		copies = append(copies, obj.DeepCopyObject().(client.Object))
	}

	return newTestBuilder(scheme, copies...).Build()
}

// crossNamespaceGrant returns a KongReferenceGrant in the gateway's namespace
// allowing fromKind from the "other" namespace to reference the gateway "gw".
func crossNamespaceGrant(fromKind string, toName *string) *configurationv1alpha1.KongReferenceGrant {
	return &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-cross-ns", Namespace: "default",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group:     "aiconfiguration.konghq.com",
				Kind:      configurationv1alpha1.Kind(fromKind),
				Namespace: "other",
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: "aigateway.konghq.com",
				Kind:  "OnPremAIGateway",
				Name:  (*configurationv1alpha1.ObjectName)(toName),
			}},
		},
	}
}

// TestBuildDocument_CrossNamespaceEntityNeedsKongReferenceGrant covers the guard on a
// cross-namespace aiGatewayRef: an entity in another namespace is translated like any
// other entity only when a KongReferenceGrant in the gateway's namespace allows its kind,
// from its namespace, to reference the OnPremAIGateway. The instance cache scopes which
// namespaces are visible (see spec.watchNamespaces); the grant decides which entities of
// those namespaces may reference the gateway.
func TestBuildDocument_CrossNamespaceEntityNeedsKongReferenceGrant(t *testing.T) {
	t.Parallel()

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// An AuthStrategy in another namespace explicitly targeting the gateway in "default":
	// the field index matches it, and the instance's cache decides whether it is listed.
	crossNamespace := aiGatewayAuthStrategyFixture("cross-ns")
	crossNamespace.Namespace = "other"
	crossNamespace.Spec.AIGatewayRef.NamespacedRef = &commonv1alpha1.NamespacedRef{
		Namespace: new("default"),
		Name:      "gw",
	}
	sameNamespace := aiGatewayAuthStrategyFixture("same-ns")

	tests := []struct {
		name         string
		grants       []client.Object
		wantIncluded bool
	}{
		{
			name: "no grant",
		},
		{
			name:         "grant for the kind and the gateway",
			grants:       []client.Object{crossNamespaceGrant("AIGatewayAuthStrategy", new("gw"))},
			wantIncluded: true,
		},
		{
			name:         "grant for the kind, any gateway",
			grants:       []client.Object{crossNamespaceGrant("AIGatewayAuthStrategy", nil)},
			wantIncluded: true,
		},
		{
			name:   "grant for another kind",
			grants: []client.Object{crossNamespaceGrant("AIGatewayModel", nil)},
		},
		{
			name:   "grant for another gateway",
			grants: []client.Object{crossNamespaceGrant("AIGatewayAuthStrategy", new("other-gw"))},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := newTranslatorTestClient(t,
				append([]client.Object{gw, crossNamespace, sameNamespace}, tt.grants...)...,
			)

			doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

			// The entity in the gateway's own namespace needs no grant.
			require.Len(t, statuses, 2)
			require.Equal(t, "same-ns", statuses[0].Obj.GetName())
			require.NoError(t, statuses[0].Err)
			require.Equal(t, "cross-ns", statuses[1].Obj.GetName())

			if !tt.wantIncluded {
				require.ErrorContains(t, statuses[1].Err, "KongReferenceGrant")
				require.Len(t, doc.AuthStrategies, 1)
				require.Equal(t, "same-ns", doc.AuthStrategies[0].Name)
				return
			}
			require.NoError(t, statuses[1].Err)
			require.Len(t, doc.AuthStrategies, 2)
			require.Equal(t, "same-ns", doc.AuthStrategies[0].Name)
			require.Equal(t, "cross-ns", doc.AuthStrategies[1].Name)
		})
	}
}

// TestBuildDocument_CrossNamespaceConsumerNeedsKongReferenceGrant pins that the guard
// covers the whole consumer, credentials included: credentials are rendered through their
// consumer, so they are reported failed with the consumer's error rather than keeping a
// stale Programmed condition.
func TestBuildDocument_CrossNamespaceConsumerNeedsKongReferenceGrant(t *testing.T) {
	t.Parallel()

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	consumer := aiGatewayConsumerFixture("cross-ns")
	consumer.Namespace = "other"
	consumer.Spec.AIGatewayRef.NamespacedRef = &commonv1alpha1.NamespacedRef{
		Namespace: new("default"),
		Name:      "gw",
	}
	credential := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Name: "cred", Namespace: "other",
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: consumer.Name,
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				Type: "api-key",
			},
		},
	}

	cl := newTranslatorTestClient(t, gw, consumer, credential)

	_, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	var consumerStatus, credentialStatus *EntityStatus
	for i := range statuses {
		switch statuses[i].Obj.GetName() {
		case consumer.Name:
			consumerStatus = &statuses[i]
		case credential.Name:
			credentialStatus = &statuses[i]
		}
	}
	require.NotNil(t, consumerStatus)
	require.ErrorContains(t, consumerStatus.Err, "KongReferenceGrant")
	require.NotNil(t, credentialStatus)
	require.ErrorContains(t, credentialStatus.Err, "KongReferenceGrant")
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
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)
	// The consumer and the credential embedded in it: the credential has no document
	// entity of its own, so the consumer's translation reports it.
	require.Len(t, statuses, 2)
	require.NoError(t, findStatusByName(t, statuses, "consumer-a").Err)
	require.NoError(t, findStatusByName(t, statuses, "cred-a").Err)
	require.Len(t, doc.Consumers, 1)
	require.Len(t, doc.Consumers[0].Credentials, 1)
	require.Equal(t, "s3cr3t", doc.Consumers[0].Credentials[0].APIKey)

	// Rotate the Secret's key value: the re-rendered document must reflect it.
	secret.Data["key"] = []byte("n3w-s3cr3t")
	require.NoError(t, cl.Update(t.Context(), secret))

	doc, statuses, err = BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	require.NoError(t, findStatusByName(t, statuses, "cred-a").Err)
	require.Len(t, doc.Consumers, 1)
	require.Equal(t, "n3w-s3cr3t", doc.Consumers[0].Credentials[0].APIKey)
}

// TestBuildDocument_ReportsBrokenCredentialOnTheCredential pins the per-credential outcome of
// the consumer's translation: a credential that fails to translate (here its secretRef Secret
// does not exist) is reported on the credential itself, and the consumer still renders with
// its healthy credentials.
func TestBuildDocument_ReportsBrokenCredentialOnTheCredential(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}

	newCredential := func(name string, secretName string) *aiconfigurationv1alpha1.AIGatewayConsumerCredential {
		return &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
			Name: name, Namespace: "default",
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
				AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "consumer-a"},
				},
				APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
					Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name),
					DisplayName: name,
					Type:        "api-key",
					APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
						Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
							Name: secretName,
							Key:  "key",
						},
					},
				},
			},
		}
	}
	healthySecret := &corev1.Secret{
		Name: "healthy-api-key", Namespace: "default",
		Data: map[string][]byte{"key": []byte("s3cr3t")},
	}

	cl := newTestBuilder(
		scheme,
		gw,
		aiGatewayConsumerFixture("consumer-a"),
		newCredential("cred-healthy", healthySecret.Name),
		// No Secret: this credential's secretRef dangles.
		newCredential("cred-broken", "missing-api-key"),
		healthySecret,
	).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	// The consumer renders with the healthy credential only.
	require.Len(t, doc.Consumers, 1)
	require.Len(t, doc.Consumers[0].Credentials, 1)
	require.Equal(t, "s3cr3t", doc.Consumers[0].Credentials[0].APIKey)

	consumer := findStatusByName(t, statuses, "consumer-a")
	require.NoError(t, consumer.Err, "a broken credential must not fail its consumer")
	require.NoError(t, findStatusByName(t, statuses, "cred-healthy").Err)

	broken := findStatusByName(t, statuses, "cred-broken")
	require.ErrorContains(t, broken.Err, "default/cred-broken")
	require.ErrorContains(t, broken.Err, "missing-api-key")
}

// newTestBuilder returns a fake client builder with every AI Gateway configuration entity
// kind's OnOnPremAIGatewayRef field index registered, ready for the test objects.
func newTestBuilder(scheme *runtime.Scheme, objs ...client.Object) *fake.ClientBuilder {
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
	for _, opt := range index.OptionsForAIGatewayEntities() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
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
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A provider with a cross-namespace secretRef and no KongReferenceGrant fails its own
	// translation while keeping its entity name intact; the healthy pair proves the model
	// failure stays per-entity.
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.ModelProviders, 1)
	require.Equal(t, "provider-healthy", doc.ModelProviders[0].Name)
	require.Len(t, doc.Models, 1)
	require.Equal(t, "model-healthy", doc.Models[0].Name)
	require.Equal(t, "provider-healthy", doc.Models[0].TargetModels[0].Provider)

	require.Len(t, statuses, 4)
	require.Contains(t, findStatusByName(t, statuses, "provider-broken").Err.Error(), "is not granted by any KongReferenceGrant")
	require.Contains(t, findStatusByName(t, statuses, "model-broken").Err.Error(),
		`referenced AIGatewayModelProvider "provider-broken" was excluded from the document`)
	// The grant denial and the exclusion it causes only fail their entities, not the gateway.
	require.True(t, IsEntityScopedFailure(findStatusByName(t, statuses, "provider-broken").Err))
	require.True(t, IsEntityScopedFailure(findStatusByName(t, statuses, "model-broken").Err))
	require.NoError(t, findStatusByName(t, statuses, "model-healthy").Err)
}

// TestBuildDocument_ModelEmbeddingsProviderResolvedByCRName pins the semantic balancer's
// embeddings provider: a bare string naming the AIGatewayModelProvider by metadata.name, which
// must reach the document as the provider's entity name (spec.apiSpec.*.name).
func TestBuildDocument_ModelEmbeddingsProviderResolvedByCRName(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	provider := aiGatewayModelProviderFixture("provider-cr")
	provider.Spec.APISpec.Openai.Name = "provider-entity"

	withEmbeddingsProvider := func(m *aiconfigurationv1alpha1.AIGatewayModel, provider string) *aiconfigurationv1alpha1.AIGatewayModel {
		m.Spec.APISpec.Model.Config.Balancer = &aiconfigurationv1alpha1.AIGatewayModelModelConfigBalancer{
			Algorithm: aiconfigurationv1alpha1.AIGatewayModelModelConfigBalancerTypeSemantic,
			Semantic: &aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfig{
				Embeddings: aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddings{
					Name:     "text-embedding-3-small",
					Provider: aiconfigurationv1alpha1.AIGatewayModelProviderReference(provider),
					Config: &aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddingsConfig{
						Type: aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddingsConfigTypeOpenai,
					},
				},
			},
		}
		return m
	}
	model := withEmbeddingsProvider(aiGatewayModelFixture("model"), "provider-cr")

	cl := newTestBuilder(scheme, gw, provider, model).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.Models, 1)
	embeddings, ok := doc.Models[0].Config.Balancer.Fields["embeddings"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "provider-entity", embeddings["provider"])
	require.NoError(t, findStatusByName(t, statuses, "model").Err)
}

// TestBuildDocument_ConsumerFailsWhenReferencedPolicyFails pins the translated-reference check
// for consumer policy references: a consumer referencing an AIGatewayPolicy that failed its
// own translation must be excluded with a per-entity error.
func TestBuildDocument_ConsumerFailsWhenReferencedPolicyFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A policy with a cross-namespace secretRef and no KongReferenceGrant fails its own
	// translation while keeping its entity name intact.
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

	// A healthy credential of the consumer that fails. The credential itself is fine, but its
	// consumer being excluded from the document keeps it out of the pushed configuration, so
	// it must be reported failed: a credential left out of the status set keeps its previous
	// condition, and the reporter only notifies the entities of the current sync.
	credentialOfBrokenConsumer := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Name: "cred-of-broken-consumer", Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "consumer-broken"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				Name:        "cred-of-broken-consumer",
				DisplayName: "Cred Of Broken Consumer",
				Type:        "api-key",
				APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new("s3cr3t"),
				},
			},
		},
	}

	cl := newTestBuilder(scheme, gw, brokenPolicy, healthyPolicy, consumerBroken, consumerHealthy, credentialOfBrokenConsumer).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.Policies, 1)
	require.Equal(t, "policy-healthy", doc.Policies[0].Name)
	require.Len(t, doc.Consumers, 1)
	require.Equal(t, "consumer-healthy", doc.Consumers[0].Name)
	require.Equal(t, []string{"policy-healthy"}, doc.Consumers[0].Policies)
	require.Empty(t, doc.Consumers[0].Credentials)

	require.Len(t, statuses, 5)
	require.Contains(t, findStatusByName(t, statuses, "policy-broken").Err.Error(), "is not granted by any KongReferenceGrant")
	require.Contains(t, findStatusByName(t, statuses, "consumer-broken").Err.Error(),
		`referenced AIGatewayPolicy "policy-broken" was excluded from the document`)
	require.NoError(t, findStatusByName(t, statuses, "consumer-healthy").Err)

	credentialStatus := findStatusByName(t, statuses, credentialOfBrokenConsumer.Name)
	require.ErrorContains(t, credentialStatus.Err,
		"its AIGatewayConsumer default/consumer-broken failed",
		"the credential reports that its consumer, not the credential, failed")
	require.ErrorContains(t, credentialStatus.Err,
		`referenced AIGatewayPolicy "policy-broken" was excluded from the document`,
		"the credential reports why its consumer was excluded")
}

// TestBuildDocument_ConsumerGroupFailsWhenReferencedPolicyFails pins the translated-reference
// check for consumer-group policy references: a consumer group referencing an AIGatewayPolicy
// that failed its own translation must be excluded with a per-entity error.
func TestBuildDocument_ConsumerGroupFailsWhenReferencedPolicyFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// A policy with a cross-namespace secretRef and no KongReferenceGrant fails its own
	// translation while keeping its entity name intact.
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.Policies, 1)
	require.Equal(t, "policy-healthy", doc.Policies[0].Name)
	require.Len(t, doc.ConsumerGroups, 1)
	require.Equal(t, "group-healthy", doc.ConsumerGroups[0].Name)
	require.Equal(t, []string{"policy-healthy"}, doc.ConsumerGroups[0].Policies)

	require.Len(t, statuses, 4)
	require.Contains(t, findStatusByName(t, statuses, "policy-broken").Err.Error(), "is not granted by any KongReferenceGrant")
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
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// An auth strategy with a cross-namespace secretRef and no KongReferenceGrant fails its
	// own translation while keeping its entity name intact.
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.AuthStrategies, 1)
	require.Equal(t, "auth-strategy-healthy", doc.AuthStrategies[0].Name)
	require.Len(t, doc.Agents, 1)
	require.Equal(t, "agent-healthy", doc.Agents[0].Name)

	require.Len(t, statuses, 6)
	require.Contains(t, findStatusByName(t, statuses, "auth-strategy-broken").Err.Error(), "is not granted by any KongReferenceGrant")
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
				Acls: &aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumerAcls{
					Type:  aiconfigurationv1alpha1.AIGatewayMCPServerListenerConsumerAclsTypeAllow,
					Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: group}},
				},
			},
		}
		return s
	}
	mcpBroken := withAccess(aiGatewayMCPServerFixture("mcp-broken"), "group-broken")
	mcpHealthy := withAccess(aiGatewayMCPServerFixture("mcp-healthy"), "group-healthy")

	cl := newTestBuilder(scheme, gw, brokenGroup, healthyGroup, healthyPolicy, mcpBroken, mcpHealthy).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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
		`referenced AIGatewayMCPServer "mcp-listener-broken" was excluded from the document: its own translation failed`)
	require.NoError(t, findStatusByName(t, statuses, "mcp-src-healthy").Err)
	require.NoError(t, findStatusByName(t, statuses, "mcp-listener-healthy").Err)
	// The source failed for a reason other than a grant denial, so the listeners that
	// depend on it still fail the gateway.
	require.False(t, IsEntityScopedFailure(findStatusByName(t, statuses, "mcp-listener-broken").Err))
	require.False(t, IsEntityScopedFailure(findStatusByName(t, statuses, "mcp-listener-cascade").Err))
}

// TestBuildDocument_MCPServerListenerOfGrantDeniedSourceIsEntityScoped pins the cause
// chain through the sources post-pass: a source excluded because its policy has no
// KongReferenceGrant only excludes the listeners that source it, also through a
// cascade, and does not fail the gateway.
func TestBuildDocument_MCPServerListenerOfGrantDeniedSourceIsEntityScoped(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	deniedPolicy := aiGatewayPolicyFixture("policy-denied")
	deniedPolicy.Spec.APISpec.Config = aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
		Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "creds", Key: "key", Namespace: new("other-ns")},
	}
	src := aiGatewayMCPServerConversionOnlyFixture("mcp-src")
	src.Spec.APISpec.ConversionOnly.Policies = []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: "policy-denied"}}
	listener := aiGatewayMCPServerListenerFixture("mcp-listener", "mcp-src")
	cascade := aiGatewayMCPServerListenerFixture("mcp-listener-cascade", "mcp-listener")

	cl := newTestBuilder(scheme, gw, deniedPolicy, src, listener, cascade).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)
	require.Empty(t, doc.MCPServers)

	for _, name := range []string{"policy-denied", "mcp-src", "mcp-listener", "mcp-listener-cascade"} {
		status := findStatusByName(t, statuses, name)
		require.Error(t, status.Err, name)
		require.True(t, IsEntityScopedFailure(status.Err), "%s: %v", name, status.Err)
	}
	require.EqualError(t, findStatusByName(t, statuses, "mcp-listener").Err,
		`referenced AIGatewayMCPServer "mcp-src" was excluded from the document: its own translation failed, or it references an excluded server`)
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
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

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	require.Len(t, doc.MCPServers, 1)
	require.Equal(t, "mcp-dup", doc.MCPServers[0].Name)

	require.Len(t, statuses, 2)
	require.Contains(t, findStatusByName(t, statuses, "mcp-dup-b").Err.Error(),
		`duplicate AIGatewayMCPServer entity name "mcp-dup"`)
	require.NoError(t, findStatusByName(t, statuses, "mcp-dup-a").Err)
}

func TestIsEntityScopedFailure(t *testing.T) {
	notGranted := &crossnamespace.ReferenceNotGrantedError{ToNamespace: "secrets", ToName: "creds"}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "model alias conflict", err: fmt.Errorf("converting: %w", &ModelAliasConflictError{Alias: "a", Route: "openai-chat", First: "m"}), want: true},
		{name: "cross-namespace reference not granted", err: fmt.Errorf("converting: %w", notGranted), want: true},
		{
			name: "reference to an entity excluded for a reference not granted",
			err:  &ReferencedEntityExcludedError{Kind: "AIGatewayPolicy", Name: "p", Cause: fmt.Errorf("converting: %w", notGranted)},
			want: true,
		},
		{
			name: "reference to an entity excluded for another failure",
			err:  &ReferencedEntityExcludedError{Kind: "AIGatewayPolicy", Name: "p", Cause: errors.New("missing key")},
		},
		{name: "reference to an entity never translated", err: &ReferencedEntityExcludedError{Kind: "AIGatewayPolicy", Name: "p"}},
		{name: "other failure", err: errors.New("spec.apiSpec is required")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsEntityScopedFailure(tt.err))
		})
	}
}

func TestEntityOutcomesRecord(t *testing.T) {
	failure := errors.New("failure")
	outcomes := make(entityOutcomes)
	record := func(name string, err error) {
		p := &aiconfigurationv1alpha1.AIGatewayPolicy{}
		p.Spec.APISpec.Name = aiconfigurationv1alpha1.AIGatewayEntityIdentifier(name)
		outcomes.record(EntityStatus{Obj: p, Err: err})
	}

	record("failed-then-ok", failure)
	record("failed-then-ok", nil)
	record("ok-then-failed", nil)
	record("ok-then-failed", failure)
	record("failed", failure)
	record("failed", errors.New("later failure"))

	// A success wins: the document holds an entity of that name.
	require.NoError(t, checkTranslatedRefs(outcomes, "AIGatewayPolicy", []string{"failed-then-ok", "ok-then-failed"}))

	err := checkTranslatedRefs(outcomes, "AIGatewayPolicy", []string{"failed"})
	require.ErrorIs(t, err, failure, "the first failure is kept as the cause")
	require.EqualError(t, err, `referenced AIGatewayPolicy "failed" was excluded from the document: its own translation failed, or it references an excluded server`)

	err = checkTranslatedRefs(outcomes, "AIGatewayPolicy", []string{"unknown"})
	require.Error(t, err)
	require.NoError(t, errors.Unwrap(err))

	// The outcomes are per kind: a policy name does not satisfy another kind.
	require.Error(t, checkTranslatedRefs(outcomes, "AIGatewayAuthStrategy", []string{"failed-then-ok"}))

	// A datastore is keyed by its CR key, not its entity name.
	d := aiGatewayDatastoreFixture("datastore")
	outcomes.record(EntityStatus{Obj: d})
	require.NoError(t, checkTranslatedDatastores(outcomes, []client.ObjectKey{client.ObjectKeyFromObject(d)}))
	require.Error(t, checkTranslatedRefs(outcomes, "AIGatewayDatastore", []string{d.GetKonnectName()}))
}

// aiGatewayDatastoreFixture returns a redis-ce datastore whose entity name is "shared-redis",
// distinct from its CR name, as the references resolve the entity name. Two CRs made by it
// therefore share the entity name.
func aiGatewayDatastoreFixture(name string) *aiconfigurationv1alpha1.AIGatewayDatastore {
	return &aiconfigurationv1alpha1.AIGatewayDatastore{
		Name: name, Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayDatastoreSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayDatastoreAPISpec{
				AIGatewayDatastoreConfig: &aiconfigurationv1alpha1.AIGatewayDatastoreConfig{
					Type: aiconfigurationv1alpha1.AIGatewayDatastoreConfigTypeRedisCE,
					RedisCE: &aiconfigurationv1alpha1.RedisCEDatastore{
						Name:   aiconfigurationv1alpha1.AIGatewayDatastoreName("shared-redis"),
						Config: aiconfigurationv1alpha1.RedisCEDatastoreConfig{Host: "redis.example.com", Port: 6379},
					},
				},
			},
		},
	}
}

// datastoreRefsReferrers returns one CR of each kind that references the given datastore:
// the datastore entity is what a Policy, a Model, a ModelProvider, an MCPServer and an
// AuthStrategy all point at.
func datastoreRefsReferrers(datastoreName string) []client.Object {
	ref := aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{Name: datastoreName}

	policy := aiGatewayPolicyFixture("policy")
	policy.Spec.APISpec.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{ref}

	model := aiGatewayModelFixture("model")
	model.Spec.APISpec.Model.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{ref}

	provider := aiGatewayModelProviderFixture("provider")
	provider.Spec.APISpec.Openai.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{ref}

	mcpServer := aiGatewayMCPServerFixture("mcp-server-a")
	mcpServer.Spec.APISpec.PassthroughListener.Policies = nil
	mcpServer.Spec.APISpec.PassthroughListener.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{ref}

	authStrategy := aiGatewayAuthStrategyFixture("auth-strategy")
	authStrategy.Spec.APISpec.KeyAuth.Datastores = []aiconfigurationv1alpha1.AIGatewayDatastoreNamespacedRef{ref}

	return []client.Object{policy, model, provider, mcpServer, authStrategy}
}

// TestBuildDocument_DatastoreRefs covers the datastores collection and its references: every
// kind that can reference a datastore resolves it to the datastore's entity name, which is what
// the converter keys its datastore registry by.
func TestBuildDocument_DatastoreRefs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	// The CR name differs from the datastore's entity name, so the assertions observe the
	// resolved name, not the name the reference was written with.
	objs := append([]client.Object{aiGatewayDatastoreFixture("datastore")},
		datastoreRefsReferrers("datastore")...)

	doc, statuses, err := BuildDocument(t.Context(), newTestBuilder(scheme, append([]client.Object{gw}, objs...)...).Build(), client.ObjectKeyFromObject(gw), convert.Options{})

	require.NoError(t, err)

	require.Len(t, doc.Datastores, 1)
	require.Equal(t, aigw.Datastore{
		Type:   "redis-ce",
		Name:   "shared-redis",
		Config: map[string]any{"host": "redis.example.com", "port": 6379},
	}, doc.Datastores[0])

	require.Len(t, doc.Policies, 1)
	require.Len(t, doc.Policies[0].Datastores, 1)
	require.Equal(t, "shared-redis", doc.Policies[0].Datastores[0].Name)
	require.Len(t, doc.Models, 1)
	require.Len(t, doc.Models[0].Datastores, 1)
	require.Equal(t, "shared-redis", doc.Models[0].Datastores[0].Name)
	require.Len(t, doc.ModelProviders, 1)
	require.Len(t, doc.ModelProviders[0].Datastores, 1)
	require.Equal(t, "shared-redis", doc.ModelProviders[0].Datastores[0].Name)
	require.Len(t, doc.MCPServers, 1)
	require.Len(t, doc.MCPServers[0].Datastores, 1)
	require.Equal(t, "shared-redis", doc.MCPServers[0].Datastores[0].Name)
	require.Len(t, doc.AuthStrategies, 1)
	require.Len(t, doc.AuthStrategies[0].Datastores, 1)
	require.Equal(t, "shared-redis", doc.AuthStrategies[0].Datastores[0].Name)

	require.Len(t, statuses, 6)
	for _, s := range statuses {
		require.NoError(t, s.Err)
	}
}

// TestBuildDocument_DatastoreFailsWhenReferencedDatastoreFails pins the translated-reference
// check for datastores: a datastore that failed its own translation is excluded, and every
// entity referencing it fails with an explicit error instead of reporting success while the
// converter leaves its connection out of the pushed payload.
func TestBuildDocument_DatastoreFailsWhenReferencedDatastoreFails(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	broken := aiGatewayDatastoreFixture("datastore")
	broken.Spec.APISpec.RedisCE.Config.Password = aiconfigurationv1alpha1.SensitiveDataSource{
		Type:      aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{Name: "does-not-exist", Key: "password"},
	}
	objs := append([]client.Object{broken}, datastoreRefsReferrers("datastore")...)

	doc, statuses, err := BuildDocument(t.Context(), newTestBuilder(scheme, append([]client.Object{gw}, objs...)...).Build(), client.ObjectKeyFromObject(gw), convert.Options{})

	require.NoError(t, err)

	require.Empty(t, doc.Datastores)
	require.Empty(t, doc.Policies)
	require.Empty(t, doc.Models)
	require.Empty(t, doc.ModelProviders)
	require.Empty(t, doc.MCPServers)
	require.Empty(t, doc.AuthStrategies)

	require.ErrorContains(t, findStatusByName(t, statuses, "datastore").Err, "does-not-exist")
	for _, name := range []string{"policy", "model", "provider", "mcp-server-a", "auth-strategy"} {
		err := findStatusByName(t, statuses, name).Err
		require.ErrorContains(t, err, `referenced AIGatewayDatastore "default/datastore" was excluded from the document`)
		// A missing Secret is not entity-scoped, so the referrers fail the gateway too.
		require.False(t, IsEntityScopedFailure(err), name)
	}
}

// TestBuildDocument_DatastoreReferrersOfGrantDeniedDatastoreAreEntityScoped pins the cause
// chain through the datastore references: a datastore excluded because no KongReferenceGrant
// allows its cross-namespace secretRef only excludes the entities that reference it.
func TestBuildDocument_DatastoreReferrersOfGrantDeniedDatastoreAreEntityScoped(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	denied := aiGatewayDatastoreFixture("datastore")
	denied.Spec.APISpec.RedisCE.Config.Password = aiconfigurationv1alpha1.SensitiveDataSource{
		Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
		SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
			Name: "redis", Key: "password", Namespace: new("secrets"),
		},
	}
	objs := append([]client.Object{gw, denied}, datastoreRefsReferrers("datastore")...)

	_, statuses, err := BuildDocument(t.Context(), newTestBuilder(scheme, objs...).Build(), client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	for _, name := range []string{"datastore", "policy", "model", "provider", "mcp-server-a", "auth-strategy"} {
		err := findStatusByName(t, statuses, name).Err
		require.Error(t, err, name)
		require.True(t, IsEntityScopedFailure(err), "%s: %v", name, err)
	}
}

// TestBuildDocument_DatastoreDuplicateEntityNameRejected: the converter resolves a datastore
// reference by name into one registry, so two AIGatewayDatastores sharing a spec name would
// leave the later one unreachable. The second CR is rejected with a per-entity error.
func TestBuildDocument_DatastoreDuplicateEntityNameRejected(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	first := aiGatewayDatastoreFixture("datastore-a")
	second := aiGatewayDatastoreFixture("datastore-b")

	doc, statuses, err := BuildDocument(t.Context(), newTestBuilder(scheme, gw, first, second).Build(), client.ObjectKeyFromObject(gw), convert.Options{})

	require.NoError(t, err)

	require.Len(t, doc.Datastores, 1)
	require.Equal(t, "shared-redis", doc.Datastores[0].Name)
	require.NoError(t, findStatusByName(t, statuses, "datastore-a").Err)
	require.ErrorContains(t, findStatusByName(t, statuses, "datastore-b").Err,
		`duplicate datastore name "shared-redis"`)
}

// TestBuildDocument_DependentOfGatewayRefDeniedEntityIsEntityScoped pins the cause chain
// of a denied cross-namespace aiGatewayRef: the denied entity never converts, so
// translateKind records its failure, and an entity referencing it is excluded as
// entity-scoped instead of failing the gateway.
func TestBuildDocument_DependentOfGatewayRefDeniedEntityIsEntityScoped(t *testing.T) {
	t.Parallel()

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	gwRef := &commonv1alpha1.NamespacedRef{Namespace: new("default"), Name: "gw"}
	provider := aiGatewayModelProviderFixture("provider-denied")
	provider.Namespace = "other"
	provider.Spec.AIGatewayRef.NamespacedRef = gwRef
	model := aiGatewayModelFixture("model-granted")
	model.Namespace = "other"
	model.Spec.AIGatewayRef.NamespacedRef = gwRef
	model.Spec.APISpec.Model.Targets = []aiconfigurationv1alpha1.AIGatewayTarget{{
		Name:     "t",
		Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{Name: "provider-denied"},
		Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
			Type:   aiconfigurationv1alpha1.AIGatewayTargetConfigTypeOpenai,
			Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{UpstreamURL: "https://api.openai.com/v1"},
		},
	}}
	// Only the model's kind may reference the gateway from the other namespace.
	cl := newTranslatorTestClient(t, gw, provider, model, crossNamespaceGrant("AIGatewayModel", nil))

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)
	require.Empty(t, doc.ModelProviders)
	require.Empty(t, doc.Models)

	providerErr := findStatusByName(t, statuses, "provider-denied").Err
	require.ErrorContains(t, providerErr, "is not granted by any KongReferenceGrant")
	require.True(t, IsEntityScopedFailure(providerErr))

	modelErr := findStatusByName(t, statuses, "model-granted").Err
	require.ErrorContains(t, modelErr, `referenced AIGatewayModelProvider "provider-denied" was excluded`)
	require.True(t, IsEntityScopedFailure(modelErr), "the model is excluded only because of the denied provider")
}

// TestBuildDocument_ModelDuplicateSpecName covers CRs that set the same spec
// model name: the converter names models by entity name in its warnings, so
// the resolution probe renames the duplicates. The attribution must stay
// exact, also for models without a selector, whose name is their alias.
func TestBuildDocument_ModelDuplicateSpecName(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	withNamed := func(crName, specName, path string, created metav1.Time) *aiconfigurationv1alpha1.AIGatewayModel {
		m := withRoutedTarget(aiGatewayModelFixture(crName))
		m.CreationTimestamp = created
		m.Spec.APISpec.Model.Name = aiconfigurationv1alpha1.AIGatewayEntityIdentifier(specName)
		m.Spec.APISpec.Model.Config.Route.Paths = []string{path}
		m.Spec.APISpec.Model.Config.Route.Model = aiconfigurationv1alpha1.AIGatewayModelSelectorConfig{
			Values: []string{"shared-alias"},
		}
		return m
	}

	gw := &aigatewayv1alpha1.OnPremAIGateway{Name: "gw", Namespace: "default"}
	dayOne := metav1.NewTime(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC))
	// Two CRs declare the spec name "dup" on ONE route path: same-route
	// conflict, the later-created cr-b loses. Two more declare it on distinct
	// route paths: cross-route sharing, both stay.
	sameA := withNamed("cr-a", "dup", "/same", dayOne)
	sameB := withNamed("cr-b", "dup", "/same", metav1.NewTime(dayOne.Add(time.Hour)))
	crossA := withNamed("cr-c", "dup", "/left", dayOne)
	crossB := withNamed("cr-d", "dup", "/right", metav1.NewTime(dayOne.Add(time.Hour)))
	// No selector: the spec name "nosel" is the alias. cr-f is created first,
	// so cr-e loses although it sorts first by name.
	noSelectorE := withNamed("cr-e", "nosel", "/nosel", metav1.NewTime(dayOne.Add(time.Hour)))
	noSelectorE.Spec.APISpec.Model.Config.Route.Model.Values = nil
	noSelectorF := withNamed("cr-f", "nosel", "/nosel", dayOne)
	noSelectorF.Spec.APISpec.Model.Config.Route.Model.Values = nil

	cl := newTestBuilder(scheme, gw, aiGatewayModelProviderFixture("provider-shared"),
		sameA, sameB, crossA, crossB, noSelectorE, noSelectorF).Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw), convert.Options{})
	require.NoError(t, err)

	names := make([]string, 0, len(doc.Models))
	for _, m := range doc.Models {
		names = append(names, m.Name)
	}
	require.ElementsMatch(t, []string{"dup", "dup", "dup", "nosel"}, names)

	loser := findStatusByName(t, statuses, "cr-b")
	require.ErrorContains(t, loser.Err, `duplicate model alias "shared-alias"`)
	require.ErrorContains(t, loser.Err, `"cr-a"`)
	loser = findStatusByName(t, statuses, "cr-e")
	require.ErrorContains(t, loser.Err, `duplicate model alias "nosel"`)
	require.ErrorContains(t, loser.Err, `"cr-f"`)
	for _, name := range []string{"cr-a", "cr-c", "cr-d", "cr-f"} {
		require.NoError(t, findStatusByName(t, statuses, name).Err, name)
	}
}

// TestCollectAliasConflicts_ConverterContract pins the ai-deck-converter
// warning the model-alias adjudication consumes. The conflict carries no
// structured details, so collectAliasConflicts matches on the severity + code +
// message triple agreed with the converter team; this test renders a real
// same-route alias conflict and asserts the issue's exact shape, so a
// converter bump that rewords the message (or moves the fields) fails here
// before the adjudication silently degrades to a plain warning. It also pins
// the negative: another ROUTING_CONFLICT emitter (the selector-shape conflict)
// must not be collected as an alias conflict.
func TestCollectAliasConflicts_ConverterContract(t *testing.T) {
	t.Parallel()

	withTarget := func(m aigw.Model) aigw.Model {
		m.Capabilities = []string{"generate"}
		m.Formats = []aigw.Format{{Type: "openai"}}
		m.TargetModels = []aigw.TargetModel{{
			Name:     "t",
			Provider: "provider-shared",
			Config: aigw.TargetModelConfig{
				Type:    "openai",
				Options: map[string]any{"upstream_url": "https://api.openai.com/v1"},
			},
		}}
		return m
	}
	provider := aigw.Provider{
		Type: "openai",
		Name: "provider-shared",
		Config: aigw.ProviderConfig{
			Auth: aigw.ProviderAuth{
				Type:    "basic",
				Headers: []aigw.AuthHeader{{Name: "apikey", Value: "k"}},
			},
		},
	}

	t.Run("the same-route alias conflict is matched", func(t *testing.T) {
		t.Parallel()

		model := func(name string) aigw.Model {
			return withTarget(aigw.Model{
				Name: name,
				Config: aigw.ModelConfig{
					Route: aigw.ModelRouteConfig{
						Paths: []string{"/chat"},
						Model: aigw.ModelAliasConfig{Values: []string{"shared-alias"}},
					},
				},
			})
		}
		doc := &aigw.Document{
			ModelProviders: []aigw.Provider{provider},
			Models:         []aigw.Model{model("model-a"), model("model-b")},
		}
		_, issues, err := RenderDBLessYAML(doc, convert.Options{})
		require.NoError(t, err)

		// The exact message the converter emits today. A converter bump that
		// rewords it must update sameRouteAliasConflictRe here and now.
		const wantReason = `model "model-b" uses the alias "shared-alias" on the route "openai-chat", ` +
			`but model "model-a" already uses it there. Both models make plugins with the same route and ai_models scope.`
		matched := 0
		for _, issue := range issues {
			if issue.Reason != wantReason {
				continue
			}
			matched++
			require.Equal(t, convert.Warning, issue.Severity)
			require.Equal(t, convert.RoutingConflict, issue.Code)
			require.Equal(t, "model", issue.Source.Kind)
			require.Equal(t, "model-b", issue.Source.Name)
			require.Equal(t, []string{"config.route.model.values"}, issue.Source.Fields)
		}
		require.Equal(t, 1, matched, "the converter's alias-conflict warning changed shape: update sameRouteAliasConflictRe")

		conflicts := collectAliasConflicts(issues)
		require.Len(t, conflicts, 1)
		require.Equal(t, "openai-chat", conflicts[0].route)
		require.Equal(t, "shared-alias", conflicts[0].alias)
		require.Equal(t, "model-a", conflicts[0].owner)
		require.Equal(t, "model-b", conflicts[0].claimer)
	})

	t.Run("the selector-shape conflict is not matched", func(t *testing.T) {
		t.Parallel()

		// Two models on one route reading the model name from different
		// sources: the converter's other model-level ROUTING_CONFLICT emitter
		// fires, and collectAliasConflicts must not treat it as an alias
		// conflict. Built from YAML: the public aigw package does not export
		// the header-selector config type.
		doc := &aigw.Document{}
		require.NoError(t, yaml.Unmarshal([]byte(`
model_providers:
  - name: provider-shared
    type: openai
    config:
      auth:
        type: basic
        headers: [{name: apikey, value: k}]
models:
  - name: model-a
    capabilities: [generate]
    formats: [{type: openai}]
    targets: [{name: t, provider: provider-shared, config: {type: openai, upstream_url: "https://api.openai.com/v1"}}]
    config:
      route:
        paths: [/chat]
        model:
          values: [alias-a]
  - name: model-b
    capabilities: [generate]
    formats: [{type: openai}]
    targets: [{name: t, provider: provider-shared, config: {type: openai, upstream_url: "https://api.openai.com/v1"}}]
    config:
      route:
        paths: [/chat]
        model:
          header:
            header_param: X-Model
`), doc))
		_, issues, err := RenderDBLessYAML(doc, convert.Options{})
		require.NoError(t, err)

		selectorConflicts := 0
		for _, issue := range issues {
			if issue.Code == convert.RoutingConflict &&
				strings.Contains(issue.Reason, "read the model name from different sources") {
				selectorConflicts++
			}
		}
		require.Positive(t, selectorConflicts, "expected the converter to emit a selector-shape conflict")

		require.Empty(t, collectAliasConflicts(issues),
			"the selector-shape conflict must not be collected as an alias conflict")
	})
}

// TestCollectAliasConflicts_QuotedValues covers values that the converter
// escapes with %q: they must parse back to the raw value.
func TestCollectAliasConflicts_QuotedValues(t *testing.T) {
	t.Parallel()

	issues := []convert.ConversionIssue{{
		Severity: convert.Warning,
		Code:     convert.RoutingConflict,
		Source:   convert.ConversionIssueSource{Kind: "model", Name: "model-b"},
		Reason: fmt.Sprintf(
			"model %q uses the alias %q on the route %q, but model %q already uses it there. "+
				"Both models make plugins with the same route and ai_models scope.",
			"model-b", `say "hi"`, "openai-chat", "model-a"),
	}}
	require.Equal(t, []aliasConflict{{
		route: "openai-chat", alias: `say "hi"`, owner: "model-a", claimer: "model-b",
	}}, collectAliasConflicts(issues))
}

// TestHasSharedAlias covers the check that skips the probe render: only an
// alias on two enabled models can conflict.
func TestHasSharedAlias(t *testing.T) {
	t.Parallel()

	model := func(name string, enabled *bool, values ...string) aigw.Model {
		return aigw.Model{
			Name:    name,
			Enabled: enabled,
			Config:  aigw.ModelConfig{Route: aigw.ModelRouteConfig{Model: aigw.ModelAliasConfig{Values: values}}},
		}
	}
	tests := []struct {
		name   string
		models []aigw.Model
		want   bool
	}{
		{name: "distinct aliases", models: []aigw.Model{model("a", nil, "x"), model("b", nil, "y")}},
		{name: "repeated value in one model", models: []aigw.Model{model("a", nil, "x", "x"), model("b", nil, "y")}},
		{name: "shared selector value", models: []aigw.Model{model("a", nil, "x"), model("b", nil, "y", "x")}, want: true},
		{name: "entity name is the alias without a selector", models: []aigw.Model{model("x", nil), model("b", nil, "x")}, want: true},
		{name: "a disabled model uses no alias", models: []aigw.Model{model("a", new(false), "x"), model("b", new(true), "x")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, hasSharedAlias(tc.models))
		})
	}
}
