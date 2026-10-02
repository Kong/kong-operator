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
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/stretchr/testify/require"
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
	sniB := aiGatewaySNIFixture("sni-b", "cert-b")
	sniA := aiGatewaySNIFixture("sni-a", "cert-a")

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, modelB, modelA, providerB, providerA, policyB, policyA, groupB, groupA,
			authStrategyB, authStrategyA, consumerB, consumerA, certB, certA, sniB, sniA)
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
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
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
	require.Len(t, doc.SNIs, 2)
	require.Equal(t, "sni-a", doc.SNIs[0].Name)
	require.Equal(t, "cert-a", doc.SNIs[0].Certificate)
	require.Equal(t, "sni-b", doc.SNIs[1].Name)
	require.Equal(t, "cert-b", doc.SNIs[1].Certificate)

	// Every entity translated successfully, so all statuses are reported as such.
	require.Len(t, statuses, 16)
	for _, s := range statuses {
		require.NoError(t, s.Err)
	}

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
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
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
	require.Empty(t, doc.Certificates)
	require.Empty(t, doc.SNIs)
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

	builder := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gw, broken, model)
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
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := BuildDocument(t.Context(), cl, client.ObjectKeyFromObject(gw))
	require.NoError(t, err)

	require.Len(t, doc.Models, 1)
	require.Equal(t, "model-a", doc.Models[0].Name)

	require.Len(t, statuses, 2)
	failed := 0
	for _, s := range statuses {
		if s.Err != nil {
			failed++
			require.Equal(t, "model-broken", s.Obj.GetName())
			require.Contains(t, s.Err.Error(), "spec.apiSpec is required")
		}
	}
	require.Equal(t, 1, failed)
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
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
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
	require.Empty(t, doc.Certificates)
	require.Empty(t, doc.SNIs)
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
