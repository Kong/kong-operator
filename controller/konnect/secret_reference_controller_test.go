package konnect

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

func TestKonnectSecretReferenceController_isSecretReferencedByKonnectResources(t *testing.T) {
	s := scheme.Get()
	testError := assert.AnError

	tests := []struct {
		name         string
		secret       *corev1.Secret
		existingObjs []client.Object
		interceptor  interceptor.Funcs
		expected     bool
		expectError  bool
	}{
		{
			name: "secret not referenced by any resources",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{},
			expected:     false,
			expectError:  false,
		},
		{
			name: "secret referenced by KonnectAPIAuthConfiguration",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{
				&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth-config",
						Namespace: "test-ns",
					},
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name:      "test-secret",
							Namespace: "test-ns",
						},
					},
				},
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "secret in different namespace not referenced",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "other-ns",
				},
			},
			existingObjs: []client.Object{
				&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth-config",
						Namespace: "test-ns",
					},
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name:      "test-secret",
							Namespace: "test-ns",
						},
					},
				},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "client error when listing resources",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{},
			interceptor: interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return testError
				},
			},
			expected:    false,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjs...)

			// Set up indexing using the same options as the real controller
			for _, opt := range index.OptionsForKonnectAPIAuthConfiguration() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}

			if tc.interceptor.List != nil {
				builder = builder.WithInterceptorFuncs(tc.interceptor)
			}

			fakeClient := builder.Build()

			controller := &KonnectSecretReferenceController{
				client: fakeClient,
			}

			result, err := controller.isSecretReferencedByKonnectResources(context.Background(), tc.secret)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestKonnectSecretReferenceController_Reconcile(t *testing.T) {
	s := scheme.Get()
	now := metav1.Now()
	pastTime := metav1.NewTime(now.Add(-10 * time.Second))
	futureTime := metav1.NewTime(now.Add(10 * time.Second))
	testError := assert.AnError

	tests := []struct {
		name                string
		secret              *corev1.Secret
		existingObjs        []client.Object
		interceptor         interceptor.Funcs
		expectedRequeue     bool
		expectError         bool
		checkFinalizer      bool
		shouldHaveFinalizer bool
	}{
		{
			name: "secret not found - should be ignored",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nonexistent-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs:    []client.Object{},
			expectedRequeue: false,
			expectError:     false,
		},
		{
			name: "secret referenced by KonnectAPIAuthConfiguration - should add finalizer",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-ns",
					},
				},
				&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth-config",
						Namespace: "test-ns",
					},
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name:      "test-secret",
							Namespace: "test-ns",
						},
					},
				},
			},
			expectedRequeue:     false,
			expectError:         false,
			checkFinalizer:      true,
			shouldHaveFinalizer: true,
		},
		{
			name: "secret not referenced - should remove finalizer if present",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-secret",
					Namespace:  "test-ns",
					Finalizers: []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-secret",
						Namespace:  "test-ns",
						Finalizers: []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			expectedRequeue:     false,
			expectError:         false,
			checkFinalizer:      true,
			shouldHaveFinalizer: false,
		},
		{
			name: "secret being deleted but still under grace period - should requeue",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-secret",
					Namespace:         "test-ns",
					DeletionTimestamp: &futureTime,
					Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret",
						Namespace:         "test-ns",
						DeletionTimestamp: &futureTime,
						Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			expectedRequeue: true,
			expectError:     false,
		},
		{
			name: "secret being deleted, grace period expired, not referenced - should remove finalizer",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-secret",
					Namespace:         "test-ns",
					DeletionTimestamp: &pastTime,
					Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret",
						Namespace:         "test-ns",
						DeletionTimestamp: &pastTime,
						Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			expectedRequeue: false,
			expectError:     false,
			// Don't check finalizer for deleted secrets as they may be removed by K8s.
			checkFinalizer:      false,
			shouldHaveFinalizer: false,
		},
		{
			name: "secret being deleted, grace period expired, still referenced - should keep finalizer",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-secret",
					Namespace:         "test-ns",
					DeletionTimestamp: &pastTime,
					Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret",
						Namespace:         "test-ns",
						DeletionTimestamp: &pastTime,
						Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
				&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth-config",
						Namespace: "test-ns",
					},
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name:      "test-secret",
							Namespace: "test-ns",
						},
					},
				},
			},
			expectedRequeue:     false,
			expectError:         false,
			checkFinalizer:      true,
			shouldHaveFinalizer: true,
		},
		{
			name: "error checking if secret is referenced",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-ns",
					},
				},
			},
			interceptor: interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
		{
			name: "error getting secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{},
			interceptor: interceptor.Funcs{
				Get: func(ctx context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
		{
			name: "error adding finalizer to referenced secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-ns",
					},
				},
				&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth-config",
						Namespace: "test-ns",
					},
					Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
						Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
						SecretRef: &corev1.SecretReference{
							Name:      "test-secret",
							Namespace: "test-ns",
						},
					},
				},
			},
			interceptor: interceptor.Funcs{
				Patch: func(ctx context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
		{
			name: "error removing finalizer from unreferenced secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-secret",
					Namespace:  "test-ns",
					Finalizers: []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-secret",
						Namespace:  "test-ns",
						Finalizers: []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			interceptor: interceptor.Funcs{
				Patch: func(ctx context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
		{
			name: "error removing finalizer from deleted unreferenced secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-secret",
					Namespace:         "test-ns",
					DeletionTimestamp: &pastTime,
					Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret",
						Namespace:         "test-ns",
						DeletionTimestamp: &pastTime,
						Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			interceptor: interceptor.Funcs{
				Patch: func(ctx context.Context, _ client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
		{
			name: "error checking if deleted secret is still referenced (grace period expired)",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-secret",
					Namespace:         "test-ns",
					DeletionTimestamp: &pastTime,
					Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
				},
			},
			existingObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret",
						Namespace:         "test-ns",
						DeletionTimestamp: &pastTime,
						Finalizers:        []string{consts.KonnectExtensionSecretInUseFinalizer},
					},
				},
			},
			interceptor: interceptor.Funcs{
				List: func(ctx context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return testError
				},
			},
			expectedRequeue: false,
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.existingObjs...)

			// Set up indexing.
			for _, opt := range index.OptionsForKonnectAPIAuthConfiguration() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}

			if tc.interceptor.List != nil || tc.interceptor.Get != nil || tc.interceptor.Patch != nil {
				builder = builder.WithInterceptorFuncs(tc.interceptor)
			}

			fakeClient := builder.Build()

			controller := &KonnectSecretReferenceController{
				client:            fakeClient,
				controllerOptions: controller.Options{},
				loggingMode:       logging.ProductionMode,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.secret.Name,
					Namespace: tc.secret.Namespace,
				},
			}

			result, err := controller.Reconcile(context.Background(), req)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tc.expectedRequeue {
				assert.Greater(t, result.RequeueAfter, time.Duration(0), "expected requeue but got none")
			} else {
				assert.Equal(t, time.Duration(0), result.RequeueAfter, "expected no requeue but got RequeueAfter: %v", result.RequeueAfter)
			}

			// Check finalizer state if specified.
			if tc.checkFinalizer {
				var secret corev1.Secret
				err := fakeClient.Get(context.Background(), req.NamespacedName, &secret)
				assert.NoError(t, err, "failed to get secret after reconcile")

				hasFinalizer := slices.Contains(secret.Finalizers, consts.KonnectExtensionSecretInUseFinalizer)

				if tc.shouldHaveFinalizer {
					assert.True(t, hasFinalizer, "expected secret to have finalizer %s but it doesn't", consts.KonnectExtensionSecretInUseFinalizer)
				} else {
					assert.False(t, hasFinalizer, "expected secret to not have finalizer %s but it does", consts.KonnectExtensionSecretInUseFinalizer)
				}
			}
		})
	}
}
