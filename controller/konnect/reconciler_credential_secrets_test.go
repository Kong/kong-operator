package konnect

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/modules/manager/scheme"

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

func TestValidateSecretForKongCredentialAPIKey(t *testing.T) {
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
					"key": []byte("api-key"),
				},
			},
			wantError: false,
		},
		{
			name: "missing key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-key",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretForKongCredentialAPIKey(tt.secret)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSecretForKongCredentialAPIKey() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateSecretForKongCredentialACL(t *testing.T) {
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
					"group": []byte("acl-group"),
				},
			},
			wantError: false,
		},
		{
			name: "missing key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-key",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretForKongCredentialACL(tt.secret)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSecretForKongCredentialACL() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateSecretForCredentialJWT(t *testing.T) {
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
					"algorithm":      []byte("RS256"),
					"key":            []byte("jwt-key"),
					"rsa_public_key": []byte("rsa-public-key"),
				},
			},
			wantError: false,
		},
		{
			name: "missing key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-key",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			wantError: true,
		},
		{
			name: "invalid algorithm",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-algorithm",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"algorithm":      []byte("INVALID"),
					"key":            []byte("jwt-key"),
					"rsa_public_key": []byte("rsa-public-key"),
				},
			},
			wantError: true,
		},
		{
			name: "missing RSA public key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-public-key",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"algorithm": []byte("RS256"),
					"key":       []byte("jwt-key"),
				},
			},
			wantError: true,
		},
		{
			name: "algorithm not requiring RSA public key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-public-key",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"algorithm": []byte("HS512"),
					"key":       []byte("jwt-key"),
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretForKongCredentialJWT(tt.secret)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSecretForKongCredentialJWT() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateSecertForKongCredentialHMAC(t *testing.T) {
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
					"username": []byte("hmac-username"),
					"secret":   []byte("hmac-secret"),
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
					"secret": []byte("hmac-secret"),
				},
			},
			wantError: true,
		},
		{
			name: "missing secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("hmac-username"),
				},
			},
			wantError: true,
		},
		{
			name: "missing username and secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-both-username-and-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecretForKongCredentialHMAC(tt.secret)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSecretForKongCredentialHMAC() error = %v, wantError %v", err, tt.wantError)
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
				_, err := ensureExistingCredential(t.Context(), client, tt.cred, tt.secret, tt.consumer)
				if (err != nil) != tt.wantError {
					t.Errorf("ensureExistingCredential() error = %v, wantError %v", err, tt.wantError)
				}
				if tt.assert != nil {
					tt.assert(t, tt.cred)
				}
			})
		}
	})

	t.Run("KongCredentialAPIKey", func(t *testing.T) {
		tests := []struct {
			name      string
			cred      *configurationv1alpha1.KongCredentialAPIKey
			secret    *corev1.Secret
			consumer  *configurationv1.KongConsumer
			assert    func(t *testing.T, cred *configurationv1alpha1.KongCredentialAPIKey)
			wantError bool
		}{
			{
				name: "credential needs update",
				cred: &configurationv1alpha1.KongCredentialAPIKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
							Key: "old-api-key",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"key": []byte("new-api-key"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				assert: func(t *testing.T, cred *configurationv1alpha1.KongCredentialAPIKey) {
					require.NotNil(t, cred)
					require.Equal(t, "new-api-key", cred.Spec.Key)
				},
				wantError: false,
			},
			{
				name: "credential does not need update",
				cred: &configurationv1alpha1.KongCredentialAPIKey{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"key": []byte("api-key"),
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
				_, err := ensureExistingCredential(t.Context(), client, tt.cred, tt.secret, tt.consumer)
				if (err != nil) != tt.wantError {
					t.Errorf("ensureExistingCredential() error = %v, wantError %v", err, tt.wantError)
				}
				if tt.assert != nil {
					tt.assert(t, tt.cred)
				}
			})
		}
	})

	t.Run("KongCredentialACL", func(t *testing.T) {
		testCases := []struct {
			name      string
			cred      *configurationv1alpha1.KongCredentialACL
			secret    *corev1.Secret
			consumer  *configurationv1.KongConsumer
			assert    func(t *testing.T, cred *configurationv1alpha1.KongCredentialACL)
			wantError bool
		}{

			{
				name: "credential needs update",
				cred: &configurationv1alpha1.KongCredentialACL{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialACLSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialACLAPISpec: configurationv1alpha1.KongCredentialACLAPISpec{
							Group: "old-acl-group",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"group": []byte("new-acl-group"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				assert: func(t *testing.T, cred *configurationv1alpha1.KongCredentialACL) {
					require.NotNil(t, cred)
					require.Equal(t, "new-acl-group", cred.Spec.Group)
				},
				wantError: false,
			},
			{
				name: "credential does not need update",
				cred: &configurationv1alpha1.KongCredentialACL{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialACLSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"group": []byte("acl-group"),
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

		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				client := clientfake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(tt.cred).
					Build()
				_, err := ensureExistingCredential(t.Context(), client, tt.cred, tt.secret, tt.consumer)
				if (err != nil) != tt.wantError {
					t.Errorf("ensureExistingCredential() error = %v, wantError %v", err, tt.wantError)
				}
				if tt.assert != nil {
					tt.assert(t, tt.cred)
				}
			})
		}
	})

	t.Run("KongCredentialJWT", func(t *testing.T) {
		testCases := []struct {
			name      string
			cred      *configurationv1alpha1.KongCredentialJWT
			secret    *corev1.Secret
			consumer  *configurationv1.KongConsumer
			assert    func(t *testing.T, cred *configurationv1alpha1.KongCredentialJWT)
			wantError bool
		}{
			{
				name: "credential needs update",
				cred: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Algorithm:    "RS256",
							Key:          lo.ToPtr("old-key"),
							RSAPublicKey: lo.ToPtr("old-public-key"),
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"algorithm":      []byte("RS256"),
						"key":            []byte("new-key"),
						"rsa_public_key": []byte("new-public-key"),
						"secret":         []byte("jwt-secret"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				assert: func(t *testing.T, cred *configurationv1alpha1.KongCredentialJWT) {
					require.NotNil(t, cred)
					require.Equal(t, "new-key", *cred.Spec.Key)
					require.Equal(t, "new-public-key", *cred.Spec.RSAPublicKey)
					require.Equal(t, "jwt-secret", *cred.Spec.Secret)
				},
				wantError: false,
			},
			{
				name: "credential does not need update",
				cred: &configurationv1alpha1.KongCredentialJWT{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialJWTSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
							Algorithm:    "RS256",
							Key:          lo.ToPtr("new-key"),
							RSAPublicKey: lo.ToPtr("new-public-key"),
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"algorithm":      []byte("RS256"),
						"key":            []byte("new-key"),
						"rsa_public_key": []byte("new-public-key"),
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
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				client := clientfake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(tt.cred).
					Build()
				_, err := ensureExistingCredential(t.Context(), client, tt.cred, tt.secret, tt.consumer)
				if (err != nil) != tt.wantError {
					t.Errorf("ensureExistingCredential() error = %v, wantError %v", err, tt.wantError)
				}
				if tt.assert != nil {
					tt.assert(t, tt.cred)
				}
			})
		}
	})

	t.Run("KongCredentialHMAC", func(t *testing.T) {
		testCases := []struct {
			name      string
			cred      *configurationv1alpha1.KongCredentialHMAC
			secret    *corev1.Secret
			consumer  *configurationv1.KongConsumer
			assert    func(t *testing.T, cred *configurationv1alpha1.KongCredentialHMAC)
			wantError bool
		}{
			{
				name: "credential needs update",
				cred: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Username: lo.ToPtr("old-hmac-username"),
							Secret:   lo.ToPtr("old-hmac-secret"),
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte("new-hmac-username"),
						"secret":   []byte("new-hmac-secret"),
					},
				},
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer",
						Namespace: "default",
					},
				},
				assert: func(t *testing.T, cred *configurationv1alpha1.KongCredentialHMAC) {
					require.NotNil(t, cred)
					require.Equal(t, lo.ToPtr("new-hmac-username"), cred.Spec.Username)
					require.Equal(t, lo.ToPtr("new-hmac-secret"), cred.Spec.Secret)
				},
				wantError: false,
			},
			{
				name: "credential does not need update",
				cred: &configurationv1alpha1.KongCredentialHMAC{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cred",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongCredentialHMACSpec{
						KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
							Username: lo.ToPtr("hmac-username"),
							Secret:   lo.ToPtr("hmac-secret"),
						},
						ConsumerRef: corev1.LocalObjectReference{
							Name: "consumer",
						},
					},
				},
				secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte("hmac-username"),
						"secret":   []byte("hmac-secret"),
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
		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				client := clientfake.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(tt.cred).
					Build()
				_, err := ensureExistingCredential(t.Context(), client, tt.cred, tt.secret, tt.consumer)
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
