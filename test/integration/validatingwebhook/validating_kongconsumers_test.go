package validatingwebhook

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/internal/annotations"
	"github.com/kong/kong-operator/modules/manager/config"
	"github.com/kong/kong-operator/test/integration"
)

func TestAdmissionWebhook_KongConsumers(t *testing.T) {
	ctx := t.Context()
	namespace, cleaner, ingressClass, ctrlClient := bootstrapGateway(
		ctx, t, integration.GetEnv(), integration.GetClients().MgrClient,
	)

	t.Logf("creating some static credentials in %s namespace which will be used to test global validation", namespace.Name)
	for _, secret := range []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tuxcreds1",
				Labels: map[string]string{
					config.DefaultSecretLabelSelector: "true",
					konnect.CredentialTypeLabel:       "basic-auth",
				},
			},
			StringData: map[string]string{
				"username": "tux1",
				"password": "testpass",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tuxcreds2",
				Labels: map[string]string{
					config.DefaultSecretLabelSelector: "true",
					konnect.CredentialTypeLabel:       "basic-auth",
				},
			},
			StringData: map[string]string{
				"username": "tux2",
				"password": "testpass",
			},
		},
	} {
		secret := secret.DeepCopy()
		require.NoError(t, ctrlClient.Create(ctx, secret))
		cleaner.Add(secret)
	}

	t.Logf("creating a static consumer in %s namespace which will be used to test global validation", namespace.Name)
	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "statis-consumer-",
			Annotations: map[string]string{
				annotations.IngressClassKey: ingressClass,
			},
		},
		Username: "tux",
		CustomID: uuid.NewString(),
		Credentials: []string{
			"tuxcreds1",
			"tuxcreds2",
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, consumer))
	cleaner.Add(consumer)

	testCases := []struct {
		name           string
		consumer       *configurationv1.KongConsumer
		credentials    []*corev1.Secret
		wantErr        bool
		wantPartialErr string
	}{
		{
			name: "a consumer with no credentials should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testconsumer",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: uuid.NewString(),
				CustomID: uuid.NewString(),
			},
			credentials: nil,
			wantErr:     false,
		},
		{
			name: "a consumer with valid credentials should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username:    "electron",
				CustomID:    uuid.NewString(),
				Credentials: []string{"electronscreds"},
			},
			credentials: []*corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "electronscreds",
					Labels: map[string]string{
						config.DefaultSecretLabelSelector: "true",
						konnect.CredentialTypeLabel:       "basic-auth",
					},
				},
				StringData: map[string]string{
					"username": "electron",
					"password": "testpass",
				},
			}},
			wantErr: false,
		},
		{
			name: "a consumer with duplicate credentials which are NOT constrained should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: "proton",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"protonscreds1",
					"protonscreds2",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "protonscreds1",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "proton",
						"password": "testpass",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "protonscreds2",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "electron", // username is unique constrained
						"password": "testpass", // password is not unique constrained
					},
				},
			},
			wantErr: false,
		},
		{
			name: "a consumer referencing credentials secrets which do not yet exist should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: "repairedlawnmower",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"nonexistentcreds",
				},
			},
			wantErr:        true,
			wantPartialErr: "not found",
		},
		{
			name: "a consumer with duplicate credentials which ARE constrained should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "brokenshovel",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: "neutron",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"neutronscreds1",
					"neutronscreds2",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "neutronscreds1",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "neutron",
						"password": "testpass",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "neutronscreds2",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "neutron", // username is unique constrained
						"password": "testpass",
					},
				},
			},
			wantErr:        true,
			wantPartialErr: "unique key constraint violated for username",
		},
		{
			name: "a consumer that provides duplicate credentials which are NOT in violation of unique key constraints should pass validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: "reasonablehammer",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"reasonablehammer",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "reasonablehammer",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "reasonablehammer",
						"password": "testpass", // not unique constrained, so even though someone else is using this password this should pass
					},
				},
			},
			wantErr: false,
		},
		{
			name: "a consumer that provides credentials that are in violation of unique constraints globally against other existing consumers should fail validation",
			consumer: &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "violating-uniqueness-",
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClass,
					},
				},
				Username: "unreasonablehammer",
				CustomID: uuid.NewString(),
				Credentials: []string{
					"unreasonablehammer",
				},
			},
			credentials: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unreasonablehammer",
						Labels: map[string]string{
							config.DefaultSecretLabelSelector: "true",
							konnect.CredentialTypeLabel:       "basic-auth",
						},
					},
					StringData: map[string]string{
						"username": "tux1", // unique constrained with previous created static consumer credentials
						"password": "testpass",
					},
				},
			},
			wantErr:        true,
			wantPartialErr: "unique key constraint violated for username",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, credential := range tc.credentials {
				require.NoError(t, ctrlClient.Create(ctx, credential))
				t.Cleanup(func() { //nolint:contextcheck
					ctx := context.Background()
					if err := ctrlClient.Delete(ctx, credential); err != nil && !apierrors.IsNotFound(err) {
						assert.NoError(t, err)
					}
				})
			}

			err := ctrlClient.Create(ctx, tc.consumer)
			if tc.wantErr {
				require.Error(t, err, "consumer %s should fail to create", tc.consumer.Name)
				assert.Contains(t, err.Error(), tc.wantPartialErr,
					"got error string %q, want a superstring of %q", err.Error(), tc.wantPartialErr,
				)
			} else {
				cleaner.Add(tc.consumer)
				t.Cleanup(func() {
					if err := ctrlClient.Delete(ctx, tc.consumer); err != nil && !apierrors.IsNotFound(err) {
						assert.NoError(t, err)
					}
				})
				require.NoError(t, err, "consumer %s should create successfully", tc.consumer.Name)
			}
		})
	}
}
