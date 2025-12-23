package konnect

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
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
			token, err := getTokenFromKonnectAPIAuthConfiguration(t.Context(), cl, tt.apiAuth)
			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			assert.Equal(t, tt.expectedToken, token)
		})
	}
}

func TestEnsureFinalizerOnKonnectAPIAuthConfiguration(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                   string
		apiAuth                *konnectv1alpha1.KonnectAPIAuthConfiguration
		referencingResources   []client.Object
		expectedFinalizerAdded bool
		expectedPatched        bool
		expectError            bool
	}{
		{
			name: "adds finalizer when KonnectGatewayControlPlane references the auth",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expectedFinalizerAdded: true,
			expectedPatched:        true,
		},
		{
			name: "adds finalizer when KonnectCloudGatewayNetwork references the auth",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			},
			referencingResources: []client.Object{
				&konnectv1alpha1.KonnectCloudGatewayNetwork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cgn",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
						Name:                          "test-cgn",
						CloudGatewayProviderAccountID: "test-provider-id",
						Region:                        "us-east-1",
						AvailabilityZones:             []string{"us-east-1a"},
						CidrBlock:                     "10.0.0.0/16",
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expectedFinalizerAdded: true,
			expectedPatched:        true,
		},
		{
			name: "adds finalizer when KonnectExtension references the auth in status",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-ext",
						Namespace: "default",
					},
					Status: konnectv1alpha2.KonnectExtensionStatus{
						Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
							ControlPlaneID: "test-cp-id",
							ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
							Endpoints: konnectv1alpha2.KonnectEndpoints{
								TelemetryEndpoint:    "https://telemetry.konghq.com",
								ControlPlaneEndpoint: "https://cp.konghq.com",
							},
							AuthRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expectedFinalizerAdded: true,
			expectedPatched:        true,
		},
		{
			name: "adds finalizer when multiple resources reference the auth",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
				&konnectv1alpha1.KonnectCloudGatewayNetwork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cgn",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
						Name:                          "test-cgn",
						CloudGatewayProviderAccountID: "test-provider-id",
						Region:                        "us-east-1",
						AvailabilityZones:             []string{"us-east-1a"},
						CidrBlock:                     "10.0.0.0/16",
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expectedFinalizerAdded: true,
			expectedPatched:        true,
		},
		{
			name: "removes finalizer when no resources reference the auth",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-auth",
					Namespace:  "default",
					Finalizers: []string{APIAuthInUseFinalizer},
				},
			},
			referencingResources:   []client.Object{},
			expectedFinalizerAdded: false,
			expectedPatched:        true,
		},
		{
			name: "does not add finalizer when auth already has it",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-auth",
					Namespace:  "default",
					Finalizers: []string{APIAuthInUseFinalizer},
				},
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expectedFinalizerAdded: true,
			expectedPatched:        false,
		},
		{
			name: "does not remove finalizer when it's not present",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			},
			referencingResources:   []client.Object{},
			expectedFinalizerAdded: false,
			expectedPatched:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scheme.Get()
			clientBuilder := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.apiAuth)

			if len(tt.referencingResources) > 0 {
				clientBuilder = clientBuilder.WithObjects(tt.referencingResources...)
			}

			// Set up indexes for all resource types
			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				index.IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					return []string{cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
				},
			)

			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha1.KonnectCloudGatewayNetwork{},
				index.IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					cgn := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
					return []string{cgn.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
				},
			)

			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha2.KonnectExtension{},
				index.IndexFieldKonnectExtensionOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					ext := obj.(*konnectv1alpha2.KonnectExtension)
					authRef := ext.GetKonnectAPIAuthConfigurationRef()
					if authRef.Name != "" {
						return []string{authRef.Name}
					}
					return nil
				},
			)

			cl := clientBuilder.Build()

			// Call the function under test
			patched, err := EnsureFinalizerOnKonnectAPIAuthConfiguration(ctx, cl, tt.apiAuth)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedPatched, patched, "patched return value mismatch")

			// Verify the finalizer state
			var updatedAuth konnectv1alpha1.KonnectAPIAuthConfiguration
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(tt.apiAuth), &updatedAuth))

			hasFinalizer := slices.Contains(updatedAuth.Finalizers, APIAuthInUseFinalizer)

			assert.Equal(t, tt.expectedFinalizerAdded, hasFinalizer, "finalizer presence mismatch")
		})
	}
}
