package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/internal/annotations"
	"github.com/kong/kong-operator/modules/manager/config"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

func TestAdmissionWebhook_SecretCredentials(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	gatewayConfig := helpers.GenerateGatewayConfiguration(namespace.Name)
	t.Logf("deploying GatewayConfiguration %s/%s", gatewayConfig.Namespace, gatewayConfig.Name)
	gatewayConfig, err := GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	gatewayClass := helpers.MustGenerateGatewayClass(t, gatewayv1.ParametersReference{
		Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
		Kind:      gatewayv1.Kind("GatewayConfiguration"),
		Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
		Name:      gatewayConfig.Name,
	})
	t.Logf("deploying GatewayClass %s", gatewayClass.Name)
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	gatewayNSN := types.NamespacedName{
		Name:      uuid.NewString(),
		Namespace: namespace.Name,
	}

	gateway := helpers.GenerateGateway(gatewayNSN, gatewayClass)
	t.Logf("deploying Gateway %s/%s", gateway.Namespace, gateway.Name)
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Logf("verifying Gateway %s/%s gets marked as Accepted", gateway.Namespace, gateway.Name)
	require.Eventually(t, testutils.GatewayIsProgrammed(t, GetCtx(), gatewayNSN, clients), 3*time.Minute, time.Second)
	t.Log("Gateway is programmed, proceeding with the test cases")

	ctrlClient := client.NewNamespacedClient(GetClients().MgrClient, namespace.Name)
	// highEndConsumerUsageCount indicates a number of consumers with credentials
	// that we consider a large number and is used to generate background
	// consumers for testing validation (since validation relies on listing all
	// consumers from the controller runtime cached client).
	const highEndConsumerUsageCount = 50
	createKongConsumers(ctx, t, ctrlClient, highEndConsumerUsageCount, gatewayClass.Name)

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
		t.Cleanup(func() {
			err := ctrlClient.Delete(ctx, validCredential)
			if err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
				assert.NoError(t, err)
			}
		})

		t.Log("verifying that valid credentials assigned to a consumer pass validation")
		validConsumerLinkedToValidCredentials := &configurationv1.KongConsumer{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "valid-consumer-",
				Annotations: map[string]string{
					annotations.IngressClassKey: gatewayClass.Name,
				},
			},
			Username: "brokenfence",
			CustomID: uuid.NewString(),
			Credentials: []string{
				"brokenfence",
			},
		}
		require.NoError(t, ctrlClient.Create(ctx, validConsumerLinkedToValidCredentials))
		t.Cleanup(func() {
			err := ctrlClient.Delete(ctx, validConsumerLinkedToValidCredentials)
			if err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
				assert.NoError(t, err)
			}
		})

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
func createKongConsumers(ctx context.Context, t *testing.T, cl client.Client, count int, gwClass string) {
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
				require.NoError(t, cl.Create(ctx, credential))
				t.Cleanup(func() {
					if err := cl.Delete(ctx, credential); err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
						assert.NoError(t, err)
					}
				})
			}

			// create the consumer referencing its credentials
			consumer := &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: consumerName,
					Annotations: map[string]string{
						annotations.IngressClassKey: gwClass,
					},
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
				assert.NoError(c, cl.Create(ctx, consumer))
			}, 10*time.Second, 100*time.Millisecond)
			t.Cleanup(func() {
				if err := cl.Delete(ctx, consumer); err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
					assert.NoError(t, err)
				}
			})
			return nil
		})
	}
	require.NoError(t, errg.Wait())
}
