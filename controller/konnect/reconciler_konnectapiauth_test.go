package konnect

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
				Name:      "test-api-auth",
				Namespace: "default",
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
				Name:      "test-api-auth",
				Namespace: "default",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				Name:      "test-secret",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/credential": "konnect",
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
				Name:      "test-api-auth",
				Namespace: "default",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				Name:      "test-secret",
				Namespace: "default",
				Data: map[string][]byte{
					"token": []byte("test-token"),
				},
			},
			expectedError: true,
		},
		{
			name: "missing token from referred Secret",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name:      "test-api-auth",
				Namespace: "default",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{
						Name:      "test-secret",
						Namespace: "default",
					},
				},
			},
			secret: &corev1.Secret{
				Name:      "test-secret",
				Namespace: "default",
				Labels: map[string]string{
					"konghq.com/credential": "konnect",
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
				Name:      "test-api-auth",
				Namespace: "default",
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
			token, err := GetTokenFromKonnectAPIAuthConfiguration(t.Context(), cl, tt.apiAuth)
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
				Name:      "test-auth",
				Namespace: "default",
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					Name:      "test-cp",
					Namespace: "default",
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
				Name:      "test-auth",
				Namespace: "default",
			},
			referencingResources: []client.Object{
				&konnectv1alpha1.KonnectCloudGatewayNetwork{
					Name:      "test-cgn",
					Namespace: "default",
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
				Name:      "test-auth",
				Namespace: "default",
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectExtension{
					Name:      "test-ext",
					Namespace: "default",
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
			name: "adds finalizer when KonnectEventGateway references the auth",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name:      "test-auth",
				Namespace: "default",
			},
			referencingResources: []client.Object{
				&konnectv1alpha1.KonnectEventGateway{
					Name:      "test-event-cp",
					Namespace: "default",
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
						APISpec: &konnectv1alpha1.KonnectEventGatewayAPISpec{
							Name: "event-cp",
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
				Name:      "test-auth",
				Namespace: "default",
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					Name:      "test-cp",
					Namespace: "default",
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
				&konnectv1alpha1.KonnectCloudGatewayNetwork{
					Name:      "test-cgn",
					Namespace: "default",
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
				Name:       "test-auth",
				Namespace:  "default",
				Finalizers: []string{APIAuthInUseFinalizer},
			},
			referencingResources:   []client.Object{},
			expectedFinalizerAdded: false,
			expectedPatched:        true,
		},
		{
			name: "does not add finalizer when auth already has it",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name:       "test-auth",
				Namespace:  "default",
				Finalizers: []string{APIAuthInUseFinalizer},
			},
			referencingResources: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					Name:      "test-cp",
					Namespace: "default",
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
				Name:      "test-auth",
				Namespace: "default",
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
					ns := cp.GetNamespace()
					authRef := cp.Spec.KonnectConfiguration.APIAuthConfigurationRef
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)

			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha1.KonnectCloudGatewayNetwork{},
				index.IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					cgn := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
					ns := cgn.GetNamespace()
					authRef := cgn.Spec.KonnectConfiguration.APIAuthConfigurationRef
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)

			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha2.KonnectExtension{},
				index.IndexFieldKonnectExtensionOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					ext := obj.(*konnectv1alpha2.KonnectExtension)
					authRef := ext.GetKonnectAPIAuthConfigurationRef()
					ns := ext.GetNamespace()
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)

			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha1.Portal{},
				index.IndexFieldPortalOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					p := obj.(*konnectv1alpha1.Portal)
					ns := p.GetNamespace()
					authRef := p.Spec.KonnectConfiguration.APIAuthConfigurationRef
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)
			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha1.KonnectEventGateway{},
				index.IndexFieldKonnectEventGatewayOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					cp := obj.(*konnectv1alpha1.KonnectEventGateway)
					ns := cp.GetNamespace()
					authRef := cp.Spec.KonnectConfiguration.APIAuthConfigurationRef
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)
			clientBuilder = clientBuilder.WithIndex(
				&konnectv1alpha1.KonnectAIGateway{},
				index.IndexFieldKonnectAIGatewayOnAPIAuthConfiguration,
				func(obj client.Object) []string {
					cp := obj.(*konnectv1alpha1.KonnectAIGateway)
					ns := cp.GetNamespace()
					authRef := cp.Spec.KonnectConfiguration.APIAuthConfigurationRef
					if authRef.Namespace != nil && *authRef.Namespace != "" {
						ns = *authRef.Namespace
					}
					if authRef.Name != "" {
						return []string{ns + "/" + authRef.Name}
					}
					return nil
				},
			)

			cl := clientBuilder.Build()

			// Call the function under test
			patched, result, err := EnsureFinalizerOnKonnectAPIAuthConfiguration(ctx, cl, tt.apiAuth)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			// Success paths must never ask the caller to requeue.
			require.Zero(t, result)
			assert.Equal(t, tt.expectedPatched, patched, "patched return value mismatch")

			// Verify the finalizer state
			var updatedAuth konnectv1alpha1.KonnectAPIAuthConfiguration
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(tt.apiAuth), &updatedAuth))

			hasFinalizer := slices.Contains(updatedAuth.Finalizers, APIAuthInUseFinalizer)

			assert.Equal(t, tt.expectedFinalizerAdded, hasFinalizer, "finalizer presence mismatch")
		})
	}
}

func TestApiAuthHasCrossNamespaceSecretRef(t *testing.T) {
	tests := []struct {
		name     string
		apiAuth  *konnectv1alpha1.KonnectAPIAuthConfiguration
		expected bool
	}{
		{
			name: "token type",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeToken,
				},
			},
			expected: false,
		},
		{
			name: "secretRef with nil SecretRef",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
				},
			},
			expected: false,
		},
		{
			name: "secretRef with empty namespace defaults to same namespace",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: ""},
				},
			},
			expected: false,
		},
		{
			name: "secretRef with same namespace",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-a"},
				},
			},
			expected: false,
		},
		{
			name: "secretRef with different namespace",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-b"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, apiAuthHasCrossNamespaceSecretRef(tt.apiAuth))
		})
	}
}

func TestReconcileSecretRefResolvedRefs(t *testing.T) {
	tests := []struct {
		name                string
		apiAuth             *konnectv1alpha1.KonnectAPIAuthConfiguration
		expectConditionSet  bool
		expectConditionTrue bool
	}{
		{
			name: "token type: no ResolvedRefs condition",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeToken,
				},
			},
			expectConditionSet: false,
		},
		{
			name: "same-namespace secretRef: no ResolvedRefs condition",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-a"},
				},
			},
			expectConditionSet: false,
		},
		{
			name: "cross-namespace secretRef: ResolvedRefs=True",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-b"},
				},
			},
			expectConditionSet:  true,
			expectConditionTrue: true,
		},
		{
			name: "cross-namespace secretRef with a stale ResolvedRefs=False is flipped to True",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-b"},
				},
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
							Status:             metav1.ConditionFalse,
							Reason:             configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectConditionSet:  true,
			expectConditionTrue: true,
		},
		{
			name: "same-namespace secretRef removes a stale ResolvedRefs condition",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "auth", Namespace: "ns-a",
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "secret", Namespace: "ns-a"},
				},
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
					Conditions: []metav1.Condition{
						{
							Type:               configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
							Status:             metav1.ConditionTrue,
							Reason:             configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectConditionSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scheme.Get()
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.apiAuth).
				WithStatusSubresource(tt.apiAuth).
				Build()

			r := &KonnectAPIAuthConfigurationReconciler{client: cl}

			_, err := r.reconcileSecretRefResolvedRefs(t.Context(), tt.apiAuth)
			require.NoError(t, err)

			var updated konnectv1alpha1.KonnectAPIAuthConfiguration
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(tt.apiAuth), &updated))

			cond, ok := k8sutils.GetCondition(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, &updated)
			if !tt.expectConditionSet {
				assert.False(t, ok, "expected no ResolvedRefs condition, got: %+v", cond)
				return
			}
			require.True(t, ok, "expected a ResolvedRefs condition to be set")
			if tt.expectConditionTrue {
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, configurationv1alpha1.KongReferenceGrantReasonResolvedRefs, cond.Reason)
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
			}
		})
	}
}

// TestReconcileKonnectAPIAuthConfigurationRateLimited asserts that a rate limited
// response from Konnect does not invalidate the credentials: the last known good
// status must be preserved, so that resources depending on this configuration
// (KonnectGatewayControlPlane, KonnectExtension, DataPlane, Gateway) are not left
// unprogrammed while Konnect is throttling the organization.
func TestReconcileKonnectAPIAuthConfigurationRateLimited(t *testing.T) {
	const existingOrganizationID = "org-id-from-previous-reconcile"

	s := scheme.Get()
	apiAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		Name:      "auth",
		Namespace: "default",
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     "token",
			ServerURL: "us.api.konghq.tech",
		},
		Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
			OrganizationID: existingOrganizationID,
			ServerURL:      "https://us.api.konghq.tech",
			Conditions: []metav1.Condition{
				{
					Type:               konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
					Status:             metav1.ConditionTrue,
					Reason:             konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
					Message:            "Token is valid",
					ObservedGeneration: 1,
				},
			},
		},
	}

	cl := withAPIAuthRefIndexes(
		fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(apiAuth),
	).WithStatusSubresource(apiAuth).Build()

	r := &KonnectAPIAuthConfigurationReconciler{
		client: cl,
		sdkFactory: sdkops.NewSDKFactory(sdkops.WithHTTPClient(&http.Client{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": []string{"30"}},
					Body:       io.NopCloser(strings.NewReader(`{"message":"API rate limit exceeded"}`)),
				}, nil
			}),
		})),
	}

	result, err := r.Reconcile(t.Context(), apiAuth)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, result.RequeueAfter)

	var updated konnectv1alpha1.KonnectAPIAuthConfiguration
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(apiAuth), &updated))

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &updated)
	require.True(t, ok, "APIAuthValid condition was removed")
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid, cond.Reason)
	assert.Equal(t, existingOrganizationID, updated.Status.OrganizationID)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// withAPIAuthRefIndexes registers the indexes that EnsureFinalizerOnKonnectAPIAuthConfiguration
// needs to list the resources referencing a KonnectAPIAuthConfiguration with a fake client.
func withAPIAuthRefIndexes(b *fake.ClientBuilder) *fake.ClientBuilder {
	b = withAPIAuthRefIndex[konnectv1alpha2.KonnectGatewayControlPlane](b, index.IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration)
	b = withAPIAuthRefIndex[konnectv1alpha1.KonnectCloudGatewayNetwork](b, index.IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration)
	b = withAPIAuthRefIndex[konnectv1alpha2.KonnectExtension](b, index.IndexFieldKonnectExtensionOnAPIAuthConfiguration)
	b = withAPIAuthRefIndex[konnectv1alpha1.KonnectAIGateway](b, index.IndexFieldKonnectAIGatewayOnAPIAuthConfiguration)
	b = withAPIAuthRefIndex[konnectv1alpha1.KonnectEventGateway](b, index.IndexFieldKonnectEventGatewayOnAPIAuthConfiguration)
	return withAPIAuthRefIndex[konnectv1alpha1.Portal](b, index.IndexFieldPortalOnAPIAuthConfiguration)
}

func withAPIAuthRefIndex[T any, TPtr interface {
	*T
	constraints.EntityWithKonnectAPIAuthConfigurationRef
}](b *fake.ClientBuilder, field string) *fake.ClientBuilder {
	obj := TPtr(new(T))
	return b.WithIndex(obj, field, func(o client.Object) []string {
		authRef := o.(TPtr).GetKonnectAPIAuthConfigurationRef()
		ns := o.GetNamespace()
		if authRef.Namespace != nil && *authRef.Namespace != "" {
			ns = *authRef.Namespace
		}
		if authRef.Name == "" {
			return nil
		}
		return []string{ns + "/" + authRef.Name}
	})
}
