package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/internal/annotations"
	"github.com/kong/kong-operator/modules/manager/config"
)

func TestAdmissionWebhook_SecretCredentials(t *testing.T) {
	t.Parallel()

	_, cleaner, ingressClass, ctrlClient := bootstrapGateway(
		t.Context(), t, env, GetClients().MgrClient,
	)

	// highEndConsumerUsageCount indicates a number of consumers with credentials
	// that we consider a large number and is used to generate background
	// consumers for testing validation (since validation relies on listing all
	// consumers from the controller runtime cached client).
	const highEndConsumerUsageCount = 50
	createKongConsumers(ctx, t, cleaner, ctrlClient, highEndConsumerUsageCount)

	t.Run("attaching secret to consumer", func(t *testing.T) {
		t.Log("verifying that a secret with unsupported but valid credential type passes the validation")
		require.NoError(t, ctrlClient.Create(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "konnect-credential",
					Labels: map[string]string{
						config.DefaultSecretLabelSelector: "true",
						konnect.CredentialTypeLabel:       "konnect",
					},
				},
				StringData: map[string]string{
					"key": "kong-credential",
				},
			},
		))

		t.Log("verifying that a secret with invalid credential type fails the validation")
		require.Error(t, ctrlClient.Create(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bad-credential",
					Labels: map[string]string{
						config.DefaultSecretLabelSelector: "true",
						konnect.CredentialTypeLabel:       "bad-type",
					},
				},
				StringData: map[string]string{
					"key": "kong-credential",
				},
			},
		))

		t.Log("verifying that an invalid credential secret not yet referenced by a KongConsumer fails validation")
		require.Error(t,
			ctrlClient.Create(ctx,
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "brokenfence",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						// missing "username" field.
						"password": "testpass",
					},
				},
			),
			"missing required field(s)",
		)

		t.Log("creating a valid credential secret to be referenced by a KongConsumer")
		validCredential := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "brokenfence",
				Labels: map[string]string{
					config.DefaultSecretLabelSelector: "true",
					konnect.CredentialTypeLabel:       "basic-auth",
				},
			},
			StringData: map[string]string{
				"username": "brokenfence",
				"password": "testpass",
			},
		}
		require.NoError(t, ctrlClient.Create(ctx, validCredential))
		cleaner.Add(validCredential)

		t.Log("verifying that valid credentials assigned to a consumer pass validation")
		validConsumerLinkedToValidCredentials := &configurationv1.KongConsumer{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "valid-consumer-",
				Annotations: map[string]string{
					annotations.IngressClassKey: ingressClass,
				},
			},
			Username: "brokenfence",
			CustomID: uuid.NewString(),
			Credentials: []string{
				"brokenfence",
			},
		}
		require.NoError(t, ctrlClient.Create(ctx, validConsumerLinkedToValidCredentials))
		cleaner.Add(validConsumerLinkedToValidCredentials)

		t.Log("verifying that the valid credentials which include a unique-constrained key can be updated in place")
		validCredential.Data["value"] = []byte("newpassword")
		require.NoError(t, ctrlClient.Update(ctx, validCredential))

		t.Log("verifying that validation fails if the now referenced and valid credential gets updated to become invalid")
		delete(validCredential.Data, "username")
		err := ctrlClient.Update(ctx, validCredential)
		require.Error(t, err)
		require.ErrorContains(t, err, "missing required field(s)")

		t.Log("verifying that if the referent consumer goes away the validation fails for updates that make the credential invalid")
		delete(validCredential.Data, "username")
		require.NoError(t, ctrlClient.Delete(ctx, validConsumerLinkedToValidCredentials))
		require.ErrorContains(t, ctrlClient.Update(ctx, validCredential), "missing required field(s)")
	})

	t.Run("JWT", func(t *testing.T) {
		t.Log("verifying that a JWT credential which has keys with missing values fails validation")
		require.ErrorContains(t,
			ctrlClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "invalid-jwt-",
					Labels: map[string]string{
						config.DefaultSecretLabelSelector: "true",
						konnect.CredentialTypeLabel:       "jwt",
					},
				},
				StringData: map[string]string{
					"algorithm": "RS256",
				},
			}),
			"missing required field(s): rsa_public_key, key",
		)

		hmacAlgos := []string{"HS256", "HS384", "HS512"}

		t.Log("verifying that a JWT credentials with hmac algorithms do not require rsa_public_key field")
		for _, algo := range hmacAlgos {
			t.Run(algo, func(t *testing.T) {
				require.NoError(t, ctrlClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-jwt-" + strings.ToLower(algo) + "-",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "jwt",
						},
					},
					StringData: map[string]string{
						"algorithm": algo,
						"key":       "key-name",
						"secret":    "secret-name",
					},
				}), "failed to create JWT credential with algorithm %s", algo)
			})
		}

		nonHmacAlgos := []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}
		t.Log("verifying that a JWT credentials with non hmac algorithms do require rsa_public_key field")
		for _, algo := range nonHmacAlgos {
			t.Run(algo, func(t *testing.T) {
				require.Error(t, ctrlClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "invalid-jwt-" + strings.ToLower(algo) + "-",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "jwt",
						},
					},
					StringData: map[string]string{
						"algorithm": algo,
						"key":       "key-name",
						"secret":    "secret-name",
					},
				}), "expected failure when creating JWT %s", algo)
			})
		}
	})
}

// createKongConsumers creates a provider number of consumers on the cluster.
// Resources will be created in client's default namespace. When using controller-runtime's
// client you can specify that by calling client.NewNamespacedClient(client, namespace).
func createKongConsumers(
	ctx context.Context, t *testing.T, cleaner *clusters.Cleaner, client client.Client, count int,
) {
	t.Helper()

	t.Logf("creating #%d of consumers on the cluster to verify the performance of the cached client during validation", count)

	errg := errgroup.Group{}
	for i := range count {
		errg.Go(func() error {
			consumerName := fmt.Sprintf("background-noise-consumer-%d", i)

			// create 5 credentials for each consumer
			for j := range 5 {
				credentialName := fmt.Sprintf("%s-credential-%d", consumerName, j)
				credential := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: credentialName,
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": credentialName,
						"password": "testpass",
					},
				}
				t.Logf("creating %s Secret that contains credentials", credentialName)
				require.NoError(t, client.Create(ctx, credential))
				cleaner.Add(credential)
			}

			// create the consumer referencing its credentials
			consumer := &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: consumerName,
				},
				Username: consumerName,
				CustomID: uuid.NewString(),
			}
			for j := range 5 {
				credentialName := fmt.Sprintf("%s-credential-%d", consumerName, j)
				consumer.Credentials = append(consumer.Credentials, credentialName)
			}
			t.Logf("creating %s KongConsumer", consumerName)
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.NoError(c, client.Create(ctx, consumer))
			}, 30*time.Second, 100*time.Millisecond)
			cleaner.Add(consumer)
			return nil
		})
	}
	require.NoError(t, errg.Wait())
}
