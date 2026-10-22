package aigateway_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	rtcontainers "github.com/kong/kong-operator/v2/test/runtimeintegration/aigateway/containers"
)

// Entity names of the representative document. Every cross-reference below
// resolves through these constants, so renaming an entity is a one-place edit.
const (
	gwName = "gw"

	providerName         = "openai-provider"
	providerCredentials  = "openai-credentials"
	modelName            = "mock-model"
	semanticModelName    = "mock-semantic-model"
	rateLimitPolicyName  = "mock-rate-limit-policy"
	customPolicyName     = "mock-custom-policy"
	customPolicyPolicy   = "mock-custom-policy-policy"
	consumerGroupName    = "mock-group"
	consumerName         = "mock-consumer"
	consumerCredential   = "mock-credential"
	consumerKeySecret    = "consumer-api-key"
	authStrategyName     = "mock-auth-strategy"
	caCertificateName    = "mock-ca-cert"
	certificateName      = "mock-cert"
	sniName              = "mock-sni"
	agentName            = "mock-agent"
	mcpServerName        = "mock-mcp-server"
	vaultName            = "mock-vault"
	upstreamURL          = "http://kong-mock.kong-aigateway-mock.svc.cluster.local:8000/provider-a"
	agentUpstreamURL     = "http://upstream:8080"
	mcpServerUpstreamURL = "http://upstream:8080"
	consumerAPIKey       = "s3cr3t"
	providerAPIKey       = "test-ai-gateway-key"
)

// Fixed test certificates, following the same convention as the ingress
// controller's golden fixtures (ingress-controller/internal/dataplane/
// testdata/golden/ingress-v1-rule-with-tls): self-signed, obviously test-only
// material guarding nothing, committed so the converter's content-derived
// certificate entity IDs — and with them the golden payload — stay
// byte-stable. The CA must remain within its validity window (the on-prem CA
// certificate translation validates parse + validity at run time); it is
// valid 2026-2066. The leaf pair is the SNI's certificate and is schema-checked
// by the Admin API for parseability and cert/key match only.
const (
	caCertPEM = `-----BEGIN CERTIFICATE-----
MIIBYzCCAQmgAwIBAgIBATAKBggqhkjOPQQDAjAgMR4wHAYDVQQDExVhaWd3LWNv
bnRyYWN0LXRlc3QtY2EwIBcNMjYwMTAxMDAwMDAwWhgPMjA2NjAxMDEwMDAwMDBa
MCAxHjAcBgNVBAMTFWFpZ3ctY29udHJhY3QtdGVzdC1jYTBZMBMGByqGSM49AgEG
CCqGSM49AwEHA0IABEUdpuDM2CZU1OGK++ppjs2EIBa84BJXFdkcKVK6gnDG/0lz
W79u/gCexOtT/ataB91GDVSV36eha4hgn58AjvajMjAwMA8GA1UdEwEB/wQFMAMB
Af8wHQYDVR0OBBYEFBTm4LDz23SgBtCDW5iClMPFfHFAMAoGCCqGSM49BAMCA0gA
MEUCIQDy5Z6iS+0f9YWs7pKSUJW3VTw3jaDzxgWxTcj856RN0QIgG5yE0gpr83DF
pVIbbkWl2asbk8j1OEmMPwgZeP1oDsA=
-----END CERTIFICATE-----`
	leafCertPEM = `-----BEGIN CERTIFICATE-----
MIIBdDCCARqgAwIBAgIBAjAKBggqhkjOPQQDAjAgMR4wHAYDVQQDExVhaWd3LWNv
bnRyYWN0LXRlc3QtY2EwIBcNMjYwMTAxMDAwMDAwWhgPMjA2NjAxMDEwMDAwMDBa
MB8xHTAbBgNVBAMTFG1vY2stc25pLmV4YW1wbGUuY29tMFkwEwYHKoZIzj0CAQYI
KoZIzj0DAQcDQgAE/GlNTDWwdGlWcNAEbwhJLZI/HUOCKW6tMvXbmGlhzBQnGMOH
LgX/8V1pCkEWhNRz5gUrTm4gCEeu625JCIk3iqNEMEIwHwYDVR0jBBgwFoAUFObg
sPPbdKAG0INbmIKUw8V8cUAwHwYDVR0RBBgwFoIUbW9jay1zbmkuZXhhbXBsZS5j
b20wCgYIKoZIzj0EAwIDSAAwRQIgMNaboK65HcuVi724cgO/LbFlfPIel+QnU/L4
X5Sn7VoCIQCtVqBFiYZv9GO+thKto5F9qkZ75H27t556D3x/F+N/rQ==
-----END CERTIFICATE-----`
	leafKeyPEM = `-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgLSTI08SAMQVRLbR4
j7LzLUpjusUqXpRayO2+J4OL+QWhRANCAAT8aU1MNbB0aVZw0ARvCEktkj8dQ4Ip
bq0y9duYaWHMFCcYw4cuBf/xXWkKQRaE1HPmBStObiAIR67rbkkIiTeK
-----END PRIVATE KEY-----`
)

func onPremRef() aiconfigurationv1alpha1.AIGatewayRef {
	return aiconfigurationv1alpha1.AIGatewayRef{
		Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
		Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gwName},
	}
}

func namespacedName(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: "default", Name: name}
}

// representativeObjects returns every object of the representative document:
// one entity of each aiconfiguration kind,
// plus their backing Secrets, cross-referenced the way a real deployment
// would be:
//
//   - model -> provider (target), rate-limiting policy, custom-policy wrapper
//     policy, key-auth auth strategy and consumer-group ACL
//   - semantic model -> provider (target, embeddings), pgvector vector database
//   - consumer -> credential (api-key), enrolled in the consumer group
//   - SNI -> certificate; the certificate is signed by the CA certificate
//   - agent and MCP server -> policy, auth strategy and consumer group
//   - vault: env backend, no references
func representativeObjects(t *testing.T) []any {
	t.Helper()

	provider := &aiconfigurationv1alpha1.AIGatewayModelProvider{
		ObjectMeta: namespacedName(providerName),
		Spec: aiconfigurationv1alpha1.AIGatewayModelProviderSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayModelProviderAPISpec{
				AIGatewayModelProviderConfig: &aiconfigurationv1alpha1.AIGatewayModelProviderConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelProviderConfigTypeOpenai,
					Openai: &aiconfigurationv1alpha1.AIGatewayModelProviderOpenai{
						Name:        providerName,
						DisplayName: "OpenAI (mock)",
						Config: aiconfigurationv1alpha1.AIGatewayModelProviderOpenaiConfig{
							Auth: aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasic{
								Headers: []aiconfigurationv1alpha1.AIGatewayModelProviderConfigAuthBasicHeaders{
									{
										Name: "apikey",
										Value: aiconfigurationv1alpha1.SensitiveDataSource{
											Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
											SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
												Name: providerCredentials,
												Key:  "apikey",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	model := &aiconfigurationv1alpha1.AIGatewayModel{
		ObjectMeta: namespacedName(modelName),
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:        modelName,
						DisplayName: "Mock model",
						// capabilities must be non-empty: without any capability
						// ai-deck-converter renders the model as a bare ai_models
						// identity row and silently emits no service/route/plugin.
						Capabilities: []string{"generate"},
						Formats:      []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{
								Paths: []string{"/chat"},
							},
						},
						Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{
							{Name: rateLimitPolicyName},
							{Name: customPolicyPolicy},
						},
						Access: aiconfigurationv1alpha1.AIGatewayModelAccess{
							AuthStrategies: []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{
								{Name: authStrategyName},
							},
							Acls: &aiconfigurationv1alpha1.AIGatewayModelAccessAcls{
								Type: aiconfigurationv1alpha1.AIGatewayModelAccessAclsTypeAllow,
								Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{
									Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: consumerGroupName}},
								},
							},
						},
						Targets: []aiconfigurationv1alpha1.AIGatewayTarget{
							{
								Name: "qwen2.5:0.5b",
								Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{
									Name: providerName,
								},
								Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
									Type: "openai",
									Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{
										UpstreamURL: upstreamURL,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// The semantic model exists for the vector database and embeddings blocks
	// alone: they are the two places where the CRD's shape (Konnect API) and
	// ai-deck-converter's document shape differ, and converter passes unknown
	// keys through, so a broken mapping only shows up as a 400 from the Admin
	// API (https://github.com/Kong/kong-operator/issues/6231).
	semanticModel := &aiconfigurationv1alpha1.AIGatewayModel{
		ObjectMeta: namespacedName(semanticModelName),
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayModelAPISpec{
				AIGatewayModelConfig: &aiconfigurationv1alpha1.AIGatewayModelConfig{
					Type: aiconfigurationv1alpha1.AIGatewayModelConfigTypeModel,
					Model: &aiconfigurationv1alpha1.AIGatewayModelModel{
						Name:         semanticModelName,
						DisplayName:  "Mock semantic model",
						Capabilities: []string{"generate"},
						Formats:      []aiconfigurationv1alpha1.AIGatewayModelFormat{{Type: "openai"}},
						Config: aiconfigurationv1alpha1.AIGatewayModelModelConfig{
							Route: aiconfigurationv1alpha1.AIGatewayModelRouteConfig{
								Paths: []string{"/semantic-chat"},
							},
							Balancer: &aiconfigurationv1alpha1.AIGatewayModelModelConfigBalancer{
								Algorithm: aiconfigurationv1alpha1.AIGatewayModelModelConfigBalancerTypeSemantic,
								Semantic: &aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfig{
									Slots: 10000,
									Embeddings: aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddings{
										AllowAuthOverride: "Enabled",
										Name:              "text-embedding-3-small",
										Provider:          providerName,
										Config: &aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddingsConfig{
											Type: aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigEmbeddingsConfigTypeOpenai,
										},
									},
									Vectordb: &aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigVectordb{
										Type: aiconfigurationv1alpha1.AIGatewayModelBalancerSemanticConfigVectordbTypePgVector,
										PgVector: &aiconfigurationv1alpha1.AIGatewayModelVectorDBConfigPgVector{
											Database:       "kong-pgvector",
											Dimensions:     1536,
											DistanceMetric: "cosine",
											Host:           "pgvector.example.com",
											Password:       "pgvector-password",
											Port:           5432,
											Threshold:      0.7,
											SSL:            aiconfigurationv1alpha1.AIGatewayModelVectorDBConfigPgVectorSSL{Enabled: "Enabled"},
										},
									},
								},
							},
						},
						Targets: []aiconfigurationv1alpha1.AIGatewayTarget{
							{
								Name: "qwen2.5:0.5b",
								Provider: aiconfigurationv1alpha1.AIGatewayModelProviderRef{
									Name: providerName,
								},
								Config: &aiconfigurationv1alpha1.AIGatewayTargetConfig{
									Type: "openai",
									Openai: &aiconfigurationv1alpha1.AIGatewayTargetOpenaiConfig{
										UpstreamURL: upstreamURL,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	rateLimitPolicy := &aiconfigurationv1alpha1.AIGatewayPolicy{
		ObjectMeta: namespacedName(rateLimitPolicyName),
		Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
				Name:        rateLimitPolicyName,
				DisplayName: "Mock rate limiting",
				Type:        "rate-limiting",
				Config: aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(jsonRaw(`{"minute": 5}`)),
				},
			},
		},
	}

	customPolicyPolicyObj := &aiconfigurationv1alpha1.AIGatewayPolicy{
		ObjectMeta: namespacedName(customPolicyPolicy),
		Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
				Name:        customPolicyPolicy,
				DisplayName: "Mock custom policy wrapper",
				CustomPolicyRef: aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{
					Name: customPolicyName,
				},
				Config: aiconfigurationv1alpha1.AIGatewayPolicyConfigDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(jsonRaw(`{}`)),
				},
			},
		},
	}

	customPolicy := &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		ObjectMeta: namespacedName(customPolicyName),
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					// streaming, not installed: an installed custom policy's
					// plugin is loaded by the data plane itself (KONG_PLUGINS +
					// a mounted handler ConfigMap), so a bare AI Gateway Admin
					// API cannot accept a plugin instance of it. A streaming
					// policy's schema and handler travel in the payload, so the
					// Admin API can validate and store it standalone.
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeStreaming,
					Streaming: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyStreamingRequest{
						Name:        customPolicyName,
						DisplayName: "Mock custom policy",
						Schema: aiconfigurationv1alpha1.ConfigMapDataSource{
							Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
							Value: new(rtcontainers.MockCustomPolicySchemaLua),
						},
						Handler: aiconfigurationv1alpha1.ConfigMapDataSource{
							Type:  aiconfigurationv1alpha1.ConfigMapDataSourceTypeInline,
							Value: new(rtcontainers.MockCustomPolicyHandlerLua),
						},
					},
				},
			},
		},
	}

	consumerGroup := &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
		ObjectMeta: namespacedName(consumerGroupName),
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerGroupSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerGroupAPISpec{
				Name:        consumerGroupName,
				DisplayName: "Mock consumer group",
			},
		},
	}

	consumer := &aiconfigurationv1alpha1.AIGatewayConsumer{
		ObjectMeta: namespacedName(consumerName),
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerAPISpec{
				Name:        consumerName,
				DisplayName: "Mock consumer",
				Type:        "api-key",
			},
			ConsumerGroups: []aiconfigurationv1alpha1.AIGatewayConsumerGroupRef{
				{Name: consumerGroupName},
			},
		},
	}

	credential := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		ObjectMeta: namespacedName(consumerCredential),
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: consumerName},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				Name:        consumerCredential,
				DisplayName: "Mock credential",
				Type:        "api-key",
				APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
					Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
						Name: consumerKeySecret,
						Key:  "key",
					},
				},
			},
		},
	}

	authStrategy := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		ObjectMeta: namespacedName(authStrategyName),
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeKeyAuth,
					KeyAuth: &aiconfigurationv1alpha1.AIGatewayAuthStrategyKeyAuth{
						Name:        authStrategyName,
						DisplayName: "Mock key-auth strategy",
					},
				},
			},
		},
	}

	caCertificate := &aiconfigurationv1alpha1.AIGatewayCACertificate{
		ObjectMeta: namespacedName(caCertificateName),
		Spec: aiconfigurationv1alpha1.AIGatewayCACertificateSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayCACertificateAPISpec{
				Name: caCertificateName,
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(caCertPEM),
				},
			},
		},
	}

	certificate := &aiconfigurationv1alpha1.AIGatewayCertificate{
		ObjectMeta: namespacedName(certificateName),
		Spec: aiconfigurationv1alpha1.AIGatewayCertificateSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayCertificateAPISpec{
				Name: certificateName,
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(leafCertPEM),
				},
				Key: aiconfigurationv1alpha1.SensitiveDataSource{
					Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
					Value: new(leafKeyPEM),
				},
			},
		},
	}

	sni := &aiconfigurationv1alpha1.AIGatewaySNI{
		ObjectMeta: namespacedName(sniName),
		Spec: aiconfigurationv1alpha1.AIGatewaySNISpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewaySNIAPISpec{
				Name:        sniName,
				DisplayName: sniName,
				Hostname:    new(aiconfigurationv1alpha1.AIGatewayHostname(sniName + ".example.com")),
				Certificate: aiconfigurationv1alpha1.AIGatewayCertificateRef{Name: certificateName},
			},
		},
	}

	agent := &aiconfigurationv1alpha1.AIGatewayAgent{
		ObjectMeta: namespacedName(agentName),
		Spec: aiconfigurationv1alpha1.AIGatewayAgentSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayAgentAPISpec{
				Name:        agentName,
				DisplayName: agentName,
				Type:        "http",
				Config: aiconfigurationv1alpha1.AIGatewayAgentConfig{
					URL: agentUpstreamURL,
					// The agent's generated route must carry a matcher: a route
					// with https in its protocols and no methods/hosts/headers/
					// paths/snis is rejected by the Admin API.
					Route: aiconfigurationv1alpha1.AIGatewayRouteConfig{
						Paths: []string{"/agent/" + agentName},
					},
				},
				Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: rateLimitPolicyName}},
				Access: aiconfigurationv1alpha1.AIGatewayAgentAccess{
					Acls: &aiconfigurationv1alpha1.AIGatewayAgentAccessAcls{
						Type:  aiconfigurationv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{{Name: consumerGroupName}}},
					},
					AuthStrategies: []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{{Name: authStrategyName}},
				},
			},
		},
	}

	mcpServer := &aiconfigurationv1alpha1.AIGatewayMCPServer{
		ObjectMeta: namespacedName(mcpServerName),
		Spec: aiconfigurationv1alpha1.AIGatewayMCPServerSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayMCPServerAPISpec{
				AIGatewayMCPServerConfig: &aiconfigurationv1alpha1.AIGatewayMCPServerConfig{
					// passthrough-listener, not conversion-only: an unexposed conversion-only
					// server is pruned by the converter with a warning, which would break the
					// pristine-render no-warnings assertion.
					Type: aiconfigurationv1alpha1.AIGatewayMCPServerConfigTypePassthroughListener,
					PassthroughListener: &aiconfigurationv1alpha1.AIGatewayMCPServerPassthroughListener{
						Name: mcpServerName,
						Config: aiconfigurationv1alpha1.AIGatewayMCPServerWithUpstreamConfig{
							URL:   mcpServerUpstreamURL,
							Route: aiconfigurationv1alpha1.AIGatewayMCPServerRouteWithMatcher{Paths: []string{"/mcp/" + mcpServerName}},
						},
						Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{{Name: rateLimitPolicyName}},
					},
				},
			},
		},
	}

	vault := &aiconfigurationv1alpha1.AIGatewayVault{
		ObjectMeta: namespacedName(vaultName),
		Spec: aiconfigurationv1alpha1.AIGatewayVaultSpec{
			AIGatewayRef: onPremRef(),
			APISpec: aiconfigurationv1alpha1.AIGatewayVaultAPISpec{
				AIGatewayVaultConfig: &aiconfigurationv1alpha1.AIGatewayVaultConfig{
					Type: aiconfigurationv1alpha1.AIGatewayVaultConfigTypeEnvironmentVariable,
					EnvironmentVariable: &aiconfigurationv1alpha1.EnvironmentVariableVault{
						Name:   vaultName,
						Config: aiconfigurationv1alpha1.EnvironmentVariableVaultConfig{Prefix: "MOCK_SECRET_"},
					},
				},
			},
		},
	}

	secrets := []*corev1.Secret{
		{
			Namespace: "default",
			Name:      providerCredentials,
			Labels:    map[string]string{"konghq.com/secret": "true"},
			Data:      map[string][]byte{"apikey": []byte(providerAPIKey)},
		},
		{
			Namespace: "default",
			Name:      consumerKeySecret,
			Labels:    map[string]string{"konghq.com/secret": "true"},
			Data:      map[string][]byte{"key": []byte(consumerAPIKey)},
		},
	}

	return []any{
		provider, model, semanticModel,
		rateLimitPolicy, customPolicyPolicyObj, customPolicy,
		consumerGroup, consumer, credential, authStrategy,
		caCertificate, certificate, sni,
		agent, mcpServer, vault,
		secrets[0], secrets[1],
	}
}

// jsonRaw wraps a raw JSON string as an apiextensionsv1.JSON value for inline
// policy configs.
func jsonRaw(s string) apiextensionsv1.JSON {
	return apiextensionsv1.JSON{Raw: []byte(s)}
}
