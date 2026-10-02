package v1alpha1

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// testCredIndexField mirrors internal/utils/index's
// IndexFieldAIGatewayConsumerCredentialOnAIGatewayConsumerRef: this api package (and its
// same-package tests) cannot import that package without a cycle, so the tests declare the
// field name and extractor locally, matching the generated extractor's
// "<refNamespace>/<refName>" value (refNamespace defaulting to the credential's namespace).
const testCredIndexField = "aiGatewayConsumerCredentialOnAIGatewayConsumerRef"

func testCredIndexExtractor(object client.Object) []string {
	cred, ok := object.(*AIGatewayConsumerCredential)
	if !ok || cred.Spec.AIGatewayConsumerRef.NamespacedRef == nil {
		return nil
	}
	ref := cred.Spec.AIGatewayConsumerRef.NamespacedRef
	ns := cred.Namespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		ns = *ref.Namespace
	}
	return []string{ns + "/" + ref.Name}
}

// TestAIGatewayConsumer_ToAIGWConsumer covers the policy and consumer group reference
// resolution — deliberately without any SetKonnectID on the referenced entities, pinning that
// the on-prem translation uses the name-only resolver, unlike the generated Konnect resolver —
// plus the managed_by drop and the credential embedding with secret-resolved api keys.
func TestAIGatewayConsumer_ToAIGWConsumer(t *testing.T) {
	t.Parallel()

	ttl3600 := 3600

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// newReferenced* return fresh objects per subtest: the fake client's tracker mutates the
	// objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newReferencedPolicy := func() *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Name: "ai-gw-policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name: "aigw-policy",
					Type: "rate-limiting",
				},
			},
		}
	}
	newReferencedConsumerGroup := func() *AIGatewayConsumerGroup {
		return &AIGatewayConsumerGroup{
			Name: "ai-gw-consumer-group", Namespace: "default",
			Spec: AIGatewayConsumerGroupSpec{
				APISpec: AIGatewayConsumerGroupAPISpec{
					Name: "dev-users",
				},
			},
		}
	}
	newReferencedSecret := func() *corev1.Secret {
		return &corev1.Secret{
			Name: "consumer-api-key", Namespace: "default",
			Data: map[string][]byte{"key": []byte("s3cr3t")},
		}
	}
	// newConsumerRef returns a namespacedRef to the consumer "sample-ai-gw-consumer" in default.
	newConsumerRef := func() commonv1alpha1.ObjectRef {
		return commonv1alpha1.ObjectRef{
			Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "sample-ai-gw-consumer"},
		}
	}
	// newConsumerCredential returns a credential referencing the consumer "sample-ai-gw-consumer"
	// in namespace default, with its apiKey backed by the Secret above.
	newConsumerCredential := func(mutators ...func(*AIGatewayConsumerCredential)) *AIGatewayConsumerCredential {
		cred := &AIGatewayConsumerCredential{
			Name: "cred-a", Namespace: "default",
			Spec: AIGatewayConsumerCredentialSpec{
				AIGatewayConsumerRef: newConsumerRef(),
				APISpec: AIGatewayConsumerCredentialAPISpec{
					Name:        "cred-1",
					DisplayName: "Cred 1",
					Type:        "api-key",
					Ttl:         3600,
					APIKey: SensitiveDataSource{
						Type: SensitiveDataSourceTypeSecretRef,
						SecretRef: &SensitiveDataSecretRef{
							Name: "consumer-api-key",
							Key:  "key",
						},
					},
				},
			},
		}
		for _, m := range mutators {
			m(cred)
		}
		return cred
	}

	tests := []struct {
		name    string
		obj     *AIGatewayConsumer
		objects []runtime.Object
		// interceptor, when set, wraps the fake client's calls (e.g. to fail a List).
		interceptor *interceptor.Funcs
		want        *aigw.Consumer
		wantErr     string
	}{
		{
			name: "policies and consumer groups resolved by name, labels kept, managed_by dropped, credentials embedded",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					ConsumerGroups: []AIGatewayConsumerGroupRef{{Name: "ai-gw-consumer-group"}},
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "sample-consumer",
						DisplayName: "Sample Consumer",
						Type:        "api-key",
						CustomID:    "custom-id",
						Labels:      PublicLabels{"app": "test1", "env": "test"},
						ManagedBy:   ManagedBy{"kong-operator": "true"},
						Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					},
				},
			},
			objects: []runtime.Object{newReferencedPolicy(), newReferencedConsumerGroup(), newReferencedSecret(), newConsumerCredential()},
			want: &aigw.Consumer{
				Name:           "sample-consumer",
				DisplayName:    "Sample Consumer",
				Type:           "api-key",
				CustomID:       "custom-id",
				Labels:         aigw.Labels{"app": "test1", "env": "test"},
				Policies:       []string{"aigw-policy"},
				ConsumerGroups: []string{"dev-users"},
				Credentials: []aigw.Credential{{
					Name:        "cred-1",
					DisplayName: "Cred 1",
					Type:        "api-key",
					TTL:         &ttl3600,
					APIKey:      "s3cr3t",
				}},
			},
		},
		{
			name: "no refs, no credentials",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-empty", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "empty-consumer",
						DisplayName: "Empty Consumer",
						Type:        "oauth",
					},
				},
			},
			objects: []runtime.Object{},
			want: &aigw.Consumer{
				Name:        "empty-consumer",
				DisplayName: "Empty Consumer",
				Type:        "oauth",
			},
		},
		{
			name: "dangling policy reference",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-dangling", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "dangling-consumer",
						DisplayName: "Dangling Consumer",
						Policies:    []AIGatewayPolicyRef{{Name: "does-not-exist"}},
					},
				},
			},
			wantErr: "policies",
		},
		{
			name: "dangling consumer group reference",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-dangling-group", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					ConsumerGroups: []AIGatewayConsumerGroupRef{{Name: "does-not-exist"}},
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "dangling-group-consumer",
						DisplayName: "Dangling Group Consumer",
					},
				},
			},
			wantErr: "consumer groups",
		},
		{
			name: "cross-namespace policy reference rejected",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-cross-ns", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "cross-ns-consumer",
						DisplayName: "Cross NS Consumer",
						Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy", Namespace: "other-namespace"}},
					},
				},
			},
			wantErr: "cross-namespace reference",
		},
		{
			name: "credential with cross-namespace secretRef rejected",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-cross-ns-secret", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "cross-ns-secret-consumer",
						DisplayName: "Cross NS Secret Consumer",
					},
				},
			},
			objects: []runtime.Object{newConsumerCredential(func(c *AIGatewayConsumerCredential) {
				c.Spec.AIGatewayConsumerRef.NamespacedRef.Name = "sample-ai-gw-consumer-cross-ns-secret"
				c.Spec.APISpec.APIKey.SecretRef.Namespace = new("other-namespace")
			})},
			wantErr: "cross-namespace secretRef",
		},
		{
			name: "credential referencing another consumer is not embedded",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer-unrelated", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "unrelated-consumer",
						DisplayName: "Unrelated Consumer",
						Type:        "oauth",
					},
				},
			},
			// Refs "sample-ai-gw-consumer", not the consumer above: filtered out by
			// the index before translation, so its secretRef's Secret is never needed.
			objects: []runtime.Object{newConsumerCredential()},
			want:    &aigw.Consumer{Name: "unrelated-consumer", DisplayName: "Unrelated Consumer", Type: "oauth"},
		},
		{
			name: "credential whose Secret is missing errors the consumer",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "missing-secret-consumer",
						DisplayName: "Missing Secret Consumer",
						Type:        "api-key",
					},
				},
			},
			// No Secret object: the credential's apiKey secretRef dangles. An existing
			// but unlabelled Secret fails the same way through the operator's filtered
			// cache, which is why the error carries the label-selector note.
			objects: []runtime.Object{newConsumerCredential()},
			// Assert the label-selector note (added by noteSecretLabelRequirement), not
			// the bare not-found: it's the part that tells an unlabelled-Secret user why.
			wantErr: "note: the operator only reads Secrets carrying its Secret label selector",
		},
		{
			name: "credential whose ref explicitly names the consumer's namespace is embedded",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "explicit-ns-consumer",
						DisplayName: "Explicit NS Consumer",
						Type:        "api-key",
					},
				},
			},
			objects: []runtime.Object{newReferencedSecret(), newConsumerCredential(func(c *AIGatewayConsumerCredential) {
				c.Spec.AIGatewayConsumerRef.NamespacedRef.Namespace = new("default")
			})},
			want: &aigw.Consumer{
				Name:        "explicit-ns-consumer",
				DisplayName: "Explicit NS Consumer",
				Type:        "api-key",
				Credentials: []aigw.Credential{{
					Name:        "cred-1",
					DisplayName: "Cred 1",
					Type:        "api-key",
					TTL:         &ttl3600,
					APIKey:      "s3cr3t",
				}},
			},
		},
		{
			name: "credential List failure errors the consumer",
			obj: &AIGatewayConsumer{
				Name: "sample-ai-gw-consumer", Namespace: "default",
				Spec: AIGatewayConsumerSpec{
					APISpec: AIGatewayConsumerAPISpec{
						Name:        "list-failure-consumer",
						DisplayName: "List Failure Consumer",
						Type:        "api-key",
					},
				},
			},
			interceptor: &interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, _ ...client.ListOption) error {
					if _, ok := list.(*AIGatewayConsumerCredentialList); ok {
						return errors.New("cache unavailable")
					}
					return nil
				},
			},
			wantErr: "listing AIGatewayConsumerCredentials: cache unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.objects...)
			if tt.interceptor != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptor)
			}
			cl := builder.WithIndex(&AIGatewayConsumerCredential{}, testCredIndexField, testCredIndexExtractor).Build()
			got, err := tt.obj.ToAIGWConsumer(t.Context(), cl, testCredIndexField)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayConsumer_ToAIGWConsumer_StrictRoundTrip guards against a dropped or renamed
// field: it decodes marshalAIGWConsumerPayload's output with yaml.v3's KnownFields(true),
// which errors on any key aigw.Consumer doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayConsumer_ToAIGWConsumer_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayConsumerAPISpec{
		Name:        "sample-consumer",
		DisplayName: "Sample Consumer",
		Labels:      PublicLabels{"app": "test1"},
		// Policies are stripped by the payload builder (they are re-attached resolved by
		// ToAIGWConsumer), so the strict decode must not see them.
		Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
	}
	data, err := spec.marshalAIGWConsumerPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var consumer aigw.Consumer
	require.NoError(t, dec.Decode(&consumer))

	require.Equal(t, "sample-consumer", consumer.Name)
	require.Equal(t, "Sample Consumer", consumer.DisplayName)
	require.Nil(t, consumer.Policies)
}

// TestAIGatewayConsumerCredential_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWConsumerCredentialPayload's output with
// yaml.v3's KnownFields(true), which errors on any key aigw.Credential doesn't
// recognize. See aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayConsumerCredential_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayConsumerCredentialAPISpec{
		Name:        "cred-1",
		DisplayName: "Cred 1",
		Type:        "api-key",
		Ttl:         3600,
		Labels:      PublicLabels{"app": "test1"},
		APIKey: SensitiveDataSource{
			Type:  SensitiveDataSourceTypeInline,
			Value: new("s3cr3t"),
		},
	}
	data, err := marshalAIGWConsumerCredentialPayload(spec)
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cred aigw.Credential
	require.NoError(t, dec.Decode(&cred))

	require.Equal(t, "cred-1", cred.Name)
	require.Equal(t, "s3cr3t", cred.APIKey)
}
