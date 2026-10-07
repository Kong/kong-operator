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

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, modelB, modelA, providerB, providerA, policyB, policyA, groupB, groupA,
			authStrategyB, authStrategyA, consumerB, consumerA, certB, certA, sniB, sniA,
			customPolicyB, customPolicyA, caCertB, caCertA)
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

	// Every entity translated successfully, so all statuses are reported as such.
	require.Len(t, statuses, 20)
	for _, s := range statuses {
		require.NoError(t, s.Err)
	}

	// Render the pristine document before the dangling provider reference is injected
	// below. A converter failure to resolve an SNI's certificate surfaces only as a
	// warning, so require none.
	sniPayload, sniWarnings, err := convert.ConvertDocumentToDBLessYAML(doc, convert.Options{Strict: false})
	require.NoError(t, err)
	require.NotEmpty(t, sniPayload)
	require.Empty(t, sniWarnings)
	// The CA certificates must reach the rendered payload with unique IDs: the converter
	// derives each entity ID from the cert content, so a tag-only match wouldn't catch an
	// ID collision. Parse the payload and check the rendered ca_certificates themselves.
	var dblessConfig struct {
		CACertificates []struct {
			ID   string   `yaml:"id"`
			Tags []string `yaml:"tags"`
		} `yaml:"ca_certificates"`
	}
	require.NoError(t, yaml.Unmarshal(sniPayload, &dblessConfig))
	require.Len(t, dblessConfig.CACertificates, 2)
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

	// Non-strict rendering must not fail even once a dangling reference is introduced by the
	// next slice - pinned here with a target that references a provider this test never creates.
	doc.Models[0].TargetModels = []aigw.TargetModel{{Name: "t", Provider: "does-not-exist"}}
	payload, warnings, err := convert.ConvertDocumentToDBLessYAML(doc, convert.Options{Strict: false})
	require.NoError(t, err)
	require.NotEmpty(t, payload)
	require.NotEmpty(t, warnings)
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
