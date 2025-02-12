package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestValidateSecretForKongCredentialBasicAuth(t *testing.T) {
	tests := []struct {
		name      string
		secret    *corev1.Secret
		wantError bool
	}{
		{
			name: "valid secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte("username"),
					corev1.BasicAuthPasswordKey: []byte("password"),
				},
			},
			wantError: false,
		},
		{
			name: "missing username",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-username",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.BasicAuthPasswordKey: []byte("password"),
				},
			},
			wantError: true,
		},
		{
			name: "missing password",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-password",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte("username"),
				},
			},
			wantError: true,
		},
		{
			name: "missing both username and password",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-both",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretForKongCredentialBasicAuth(tt.secret)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSecretForKongCredentialBasicAuth() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestEnsureExistingCredential(t *testing.T) {
	t.Run("KongCredentialBasicAuth", func(t *testing.T) {
		tests := []struct {
			name      string
			cred      *configurationv1alpha1.KongCredentialBasicAuth
			secret    *corev1.Secret
			consumer  *configurationv1.KongConsumer
			assert    func(t *testing.T, cred *configurationv1alpha1.KongCredentialBasicAuth)
			wantError bool
		}{
			{
				name: "credential needs update",
				cred: &configurationv1alpha1.KongCredentialBasicAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
							Username: "old-username",
							Password: "old-password",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						corev1.BasicAuthUsernameKey: []byte("new-username"),
						corev1.BasicAuthPasswordKey: []byte("new-password"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				assert: func(t *testing.T, cred *configurationv1alpha1.KongCredentialBasicAuth) {
					require.NotNil(t, cred)
					require.Equal(t, "new-username", cred.Spec.Username)
					require.Equal(t, "new-password", cred.Spec.Password)
				},
				wantError: false,
			},
			{
				name: "credential does not need update",
				cred: &configurationv1alpha1.KongCredentialBasicAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
							Username: "username",
							Password: "password",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						corev1.BasicAuthUsernameKey: []byte("username"),
						corev1.BasicAuthPasswordKey: []byte("password"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				wantError: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client := clientfake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(tt.cred).
					Build()
				_, err := ensureExistingCredential(context.Background(), client, tt.cred, tt.secret, tt.consumer)
				if (err != nil) != tt.wantError {
					t.Errorf("ensureExistingCredential() error = %v, wantError %v", err, tt.wantError)
				}
				if tt.assert != nil {
					tt.assert(t, tt.cred)
				}
			})
		}
	})
}
