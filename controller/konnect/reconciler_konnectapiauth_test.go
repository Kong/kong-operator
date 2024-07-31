package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestGetTokenFromKonnectAPIAuthConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		apiAuth       *konnectv1alpha1.KonnectAPIAuthConfiguration
		secret        *corev1.Secret
		expectedToken string
		expectedError bool
	}{
		{
			name: "valid Token",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-api-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:  konnectv1alpha1.KonnectAPIAuthTypeToken,
					Token: "kpat_xxxxxxxxxxxx",
				},
			},
			expectedToken: "kpat_xxxxxxxxxxxx",
		},
		{
			name: "valid Secret Reference",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-api-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					Labels: map[string]string{
						"konghq.com/credential": "konnect",
					},
				},
				Data: map[string][]byte{
					"token": []byte("test-token"),
				},
			},
			expectedToken: "test-token",
		},
		{
			name: "Secret is missing konghq.com/credential=konnect label",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-api-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("test-token"),
				},
			},
			expectedError: true,
		},
		{
			name: "missing token from referred Secret",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-api-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					Labels: map[string]string{
						"konghq.com/credential": "konnect",
					},
				},
				Data: map[string][]byte{
					"random_key": []byte("dummy"),
				},
			},
			expectedToken: "test-token",
			expectedError: true,
		},
		{
			name: "Invalid Secret Reference",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-api-auth",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "non-existent-secret",
						Namespace: "default",
					},
				},
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder()

			// Create the secret in the fake client
			if tt.secret != nil {
				clientBuilder.WithObjects(tt.secret)
			}
			cl := clientBuilder.Build()

			// Call the function under test
			token, err := getTokenFromKonnectAPIAuthConfiguration(context.Background(), cl, tt.apiAuth)
			if tt.expectedError {
				assert.NotNil(t, err)
				return
			}

			assert.Equal(t, tt.expectedToken, token)
		})
	}
}
