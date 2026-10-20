package konnect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestHandleSecretRef(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Get()

	testCases := []struct {
		name                string
		certificate         *configurationv1alpha1.KongCertificate
		secrets             []corev1.Secret
		grants              []configurationv1alpha1.KongReferenceGrant
		expectResult        bool
		expectError         bool
		expectConditionType consts.ConditionType
		expectCondition     metav1.ConditionStatus
		expectReason        string
	}{
		{
			name: "secret exists in same namespace",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "default",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "test-secret",
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "default",
				},
			},
			expectResult:        false,
			expectError:         false,
			expectConditionType: konnectv1alpha1.SecretRefValidConditionType,
			expectCondition:     metav1.ConditionTrue,
			expectReason:        konnectv1alpha1.SecretRefReasonValid,
		},
		{
			name: "secret does not exist",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "default",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "missing-secret",
					},
				},
			},
			secrets:             []corev1.Secret{},
			expectResult:        true,
			expectError:         true,
			expectConditionType: konnectv1alpha1.SecretRefValidConditionType,
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.SecretRefReasonInvalid,
		},
		{
			name: "cross-namespace reference with valid grant",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "cert-ns",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					Name:      "allow-cert-to-secret",
					Namespace: "secret-ns",
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  new(configurationv1alpha1.ObjectName("test-secret")),
							},
						},
					},
				},
			},
			expectResult:        false,
			expectError:         false,
			expectConditionType: consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			expectCondition:     metav1.ConditionTrue,
			expectReason:        configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		},
		{
			name: "cross-namespace reference without grant",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "cert-ns",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
			},
			grants:              []configurationv1alpha1.KongReferenceGrant{},
			expectResult:        true,
			expectError:         false,
			expectConditionType: consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
		},
		{
			name: "cross-namespace reference with grant for wrong namespace",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "cert-ns",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					Name:      "allow-cert-to-secret",
					Namespace: "secret-ns",
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "other-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  new(configurationv1alpha1.ObjectName("test-secret")),
							},
						},
					},
				},
			},
			expectResult:        true,
			expectError:         false,
			expectConditionType: consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
		},
		{
			name: "multiple secret refs with grants",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "cert-ns",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret-alt",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
				{
					Name:      "test-secret-alt",
					Namespace: "secret-ns",
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					Name:      "allow-cert-to-secrets",
					Namespace: "secret-ns",
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  nil, // Allow all secrets
							},
						},
					},
				},
			},
			expectResult:        false,
			expectError:         false,
			expectConditionType: consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			expectCondition:     metav1.ConditionTrue,
			expectReason:        configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		},
		{
			name: "multiple secret refs one missing grant",
			certificate: &configurationv1alpha1.KongCertificate{
				Name:       "test-cert",
				Namespace:  "cert-ns",
				APIVersion: configurationv1alpha1.GroupVersion.String(),
				Kind:       "KongCertificate",
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
					SecretRefAlt: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret-alt",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
				{
					Name:      "test-secret-alt",
					Namespace: "secret-ns",
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					Name:      "allow-cert-to-secret",
					Namespace: "secret-ns",
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  new(configurationv1alpha1.ObjectName("test-secret")),
							},
						},
					},
				},
			},
			expectResult:        true,
			expectError:         false,
			expectConditionType: consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
		},
		{
			name: "secret missing during deletion does not block cleanup",
			certificate: func() *configurationv1alpha1.KongCertificate {
				now := metav1.Now()
				return &configurationv1alpha1.KongCertificate{
					Name:              "test-cert",
					Namespace:         "default",
					DeletionTimestamp: &now,
					Finalizers:        []string{KonnectCleanupFinalizer},
					APIVersion:        configurationv1alpha1.GroupVersion.String(),
					Kind:              "KongCertificate",
					Spec: configurationv1alpha1.KongCertificateSpec{
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name: "missing-secret",
						},
					},
				}
			}(),
			secrets:      []corev1.Secret{},
			expectResult: false,
			expectError:  false,
		},
		{
			name: "cross-namespace ref without grant during deletion does not block cleanup",
			certificate: func() *configurationv1alpha1.KongCertificate {
				now := metav1.Now()
				return &configurationv1alpha1.KongCertificate{
					Name:              "test-cert",
					Namespace:         "cert-ns",
					DeletionTimestamp: &now,
					Finalizers:        []string{KonnectCleanupFinalizer},
					APIVersion:        configurationv1alpha1.GroupVersion.String(),
					Kind:              "KongCertificate",
					Spec: configurationv1alpha1.KongCertificateSpec{
						SecretRef: &commonv1alpha1.NamespacedRef{
							Name:      "test-secret",
							Namespace: new("secret-ns"),
						},
					},
				}
			}(),
			secrets: []corev1.Secret{
				{
					Name:      "test-secret",
					Namespace: "secret-ns",
				},
			},
			grants:       []configurationv1alpha1.KongReferenceGrant{},
			expectResult: false,
			expectError:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			for i := range tc.secrets {
				objs = append(objs, &tc.secrets[i])
			}
			for i := range tc.grants {
				objs = append(objs, &tc.grants[i])
			}
			objs = append(objs, tc.certificate)

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(tc.certificate).
				Build()

			result, hasResult, err := handleSecretRef(ctx, cl, tc.certificate)

			assert.Equal(t, tc.expectResult, hasResult, "unexpected hasResult value")
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc.expectConditionType != "" {
				// Refresh the certificate to get updated status
				updatedCert := &configurationv1alpha1.KongCertificate{}
				err := cl.Get(ctx, client.ObjectKeyFromObject(tc.certificate), updatedCert)
				require.NoError(t, err)

				var found bool
				for _, cond := range updatedCert.Status.Conditions {
					if cond.Type == string(tc.expectConditionType) {
						found = true
						assert.Equal(t, tc.expectCondition, cond.Status, "unexpected condition status")
						assert.Equal(t, tc.expectReason, cond.Reason, "unexpected condition reason")
						break
					}
				}
				assert.True(t, found, "expected condition type %s not found", tc.expectConditionType)
			}

			if !tc.expectError && tc.expectResult {
				assert.True(t, result.IsZero(), "expected zero result when returning early without error")
			}
		})
	}
}

// TestHandleSecretRef_RecoversToValidAfterFix ensures that once a referenced
// Secret is fixed (created after being missing), SecretRefValid flips back to
// True on the next reconcile instead of staying stuck at False forever.
func TestHandleSecretRef_RecoversToValidAfterFix(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Get()

	cert := &configurationv1alpha1.KongCertificate{
		Name:       "test-cert",
		Namespace:  "default",
		APIVersion: configurationv1alpha1.GroupVersion.String(),
		Kind:       "KongCertificate",
		Spec: configurationv1alpha1.KongCertificateSpec{
			SecretRef: &commonv1alpha1.NamespacedRef{
				Name: "test-secret",
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cert).
		WithStatusSubresource(cert).
		Build()

	// First reconcile: the Secret doesn't exist yet, condition must go False.
	_, hasResult, err := handleSecretRef(ctx, cl, cert)
	require.Error(t, err)
	assert.True(t, hasResult)

	updated := &configurationv1alpha1.KongCertificate{}
	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cert), updated))
	cond, found := findCondition(updated, string(konnectv1alpha1.SecretRefValidConditionType))
	require.True(t, found)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, konnectv1alpha1.SecretRefReasonInvalid, cond.Reason)

	// The Secret gets created; the next reconcile must flip the condition back
	// to True instead of leaving the stale False value in place.
	secret := &corev1.Secret{
		Name:      "test-secret",
		Namespace: "default",
	}
	require.NoError(t, cl.Create(ctx, secret))

	_, hasResult, err = handleSecretRef(ctx, cl, updated)
	require.NoError(t, err)
	assert.False(t, hasResult)

	require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cert), updated))
	cond, found = findCondition(updated, string(konnectv1alpha1.SecretRefValidConditionType))
	require.True(t, found)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, konnectv1alpha1.SecretRefReasonValid, cond.Reason)
}

// TestHandleSecretRef_SensitiveDataSourceCrossNamespace ensures the
// KongReferenceGrant check is enforced for cross-namespace secretRef on
// entities that use the generated SensitiveDataSource mechanism (detected via
// GetSensitiveDataSecretRefs), not just on the hand-written KongCertificate
// cases above. EventGatewayBackendCluster is used as a representative type.
func TestHandleSecretRef_SensitiveDataSourceCrossNamespace(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Get()

	newCluster := func() *configurationv1alpha1.EventGatewayBackendCluster {
		return &configurationv1alpha1.EventGatewayBackendCluster{
			Name:       "test-cluster",
			Namespace:  "cluster-ns",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayBackendCluster",
			Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
				APISpec: configurationv1alpha1.EventGatewayBackendClusterAPISpec{
					TLS: configurationv1alpha1.BackendClusterTLS{
						ClientIdentity: configurationv1alpha1.BackendClusterTLSClientIdentity{
							Certificate: configurationv1alpha1.SensitiveDataSource{
								Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
									Name:      "test-secret",
									Key:       "tls.crt",
									Namespace: new("secret-ns"),
								},
							},
						},
					},
				},
			},
		}
	}
	secret := &corev1.Secret{
		Name:      "test-secret",
		Namespace: "secret-ns",
		Data:      map[string][]byte{"tls.crt": []byte("cert")},
	}

	t.Run("without grant is not permitted", func(t *testing.T) {
		cluster := newCluster()
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, secret).
			WithStatusSubresource(cluster).
			Build()

		_, hasResult, err := handleSecretRef(ctx, cl, cluster)
		require.NoError(t, err)
		assert.True(t, hasResult)

		updated := &configurationv1alpha1.EventGatewayBackendCluster{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updated))
		cond, found := findConditionGeneric(updated.Status.Conditions, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
		require.True(t, found)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted, cond.Reason)
	})

	t.Run("with matching grant is permitted", func(t *testing.T) {
		cluster := newCluster()
		grant := &configurationv1alpha1.KongReferenceGrant{
			Name:      "allow-cluster-to-secret",
			Namespace: "secret-ns",
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{
					{
						Group:     "configuration.konghq.com",
						Kind:      "EventGatewayBackendCluster",
						Namespace: "cluster-ns",
					},
				},
				To: []configurationv1alpha1.ReferenceGrantTo{
					{
						Group: "core",
						Kind:  "Secret",
						Name:  new(configurationv1alpha1.ObjectName("test-secret")),
					},
				},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster, secret, grant).
			WithStatusSubresource(cluster).
			Build()

		_, hasResult, err := handleSecretRef(ctx, cl, cluster)
		require.NoError(t, err)
		assert.False(t, hasResult)

		updated := &configurationv1alpha1.EventGatewayBackendCluster{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(cluster), updated))
		cond, found := findConditionGeneric(updated.Status.Conditions, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
		require.True(t, found)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, configurationv1alpha1.KongReferenceGrantReasonResolvedRefs, cond.Reason)
	})
}

func findConditionGeneric(conditions []metav1.Condition, condType string) (metav1.Condition, bool) {
	for _, cond := range conditions {
		if cond.Type == condType {
			return cond, true
		}
	}
	return metav1.Condition{}, false
}

func findCondition(cert *configurationv1alpha1.KongCertificate, condType string) (metav1.Condition, bool) {
	for _, cond := range cert.Status.Conditions {
		if cond.Type == condType {
			return cond, true
		}
	}
	return metav1.Condition{}, false
}

// TestHandleSecretRef_StaticKeyReadOnlyAtCreation ensures an
// EventGatewayStaticKey needs its Secret to be created in Konnect, but not
// afterwards: static keys can't be updated, so a missing Secret only matters
// to recreate the key. Access to a cross-namespace Secret is always checked.
func TestHandleSecretRef_StaticKeyReadOnlyAtCreation(t *testing.T) {
	ctx := context.Background()

	newStaticKey := func(secretNamespace *string) *configurationv1alpha1.EventGatewayStaticKey {
		return &configurationv1alpha1.EventGatewayStaticKey{
			Name:       "static-key",
			Namespace:  "default",
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayStaticKey",
			Spec: configurationv1alpha1.EventGatewayStaticKeySpec{
				APISpec: configurationv1alpha1.EventGatewayStaticKeyAPISpec{
					Name: "static-key",
					Value: configurationv1alpha1.SensitiveDataSource{
						Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "key", Key: "key", Namespace: secretNamespace},
					},
				},
			},
		}
	}
	newClient := func(obj client.Object, objs ...client.Object) client.Client {
		return fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(append(objs, obj)...).WithStatusSubresource(obj).Build()
	}
	secretRefValid := func(t *testing.T, cl client.Client, key *configurationv1alpha1.EventGatewayStaticKey) metav1.Condition {
		t.Helper()
		updated := &configurationv1alpha1.EventGatewayStaticKey{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(key), updated))
		cond, found := findConditionGeneric(updated.Status.Conditions, string(konnectv1alpha1.SecretRefValidConditionType))
		require.True(t, found)
		return cond
	}

	t.Run("requires the Secret before creation", func(t *testing.T) {
		key := newStaticKey(nil)
		_, stop, err := handleSecretRef(ctx, newClient(key), key)
		require.Error(t, err)
		assert.True(t, stop)
	})

	t.Run("reports but doesn't require a missing Secret once created", func(t *testing.T) {
		key := newStaticKey(nil)
		key.SetKonnectID("static-key-id")
		cl := newClient(key)
		res, stop, err := handleSecretRef(ctx, cl, key)
		require.NoError(t, err)
		assert.False(t, stop)
		assert.True(t, res.IsZero())
		cond := secretRefValid(t, cl, key)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Contains(t, cond.Message, "Secret default/key not found")
		assert.Contains(t, cond.Message, "required to recreate it")
	})

	t.Run("stops once created on errors other than a missing Secret", func(t *testing.T) {
		key := newStaticKey(nil)
		key.SetKonnectID("static-key-id")
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(key).
			WithStatusSubresource(key).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, k client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Secret); ok {
						return errors.New("cache not synced")
					}
					return c.Get(ctx, k, obj, opts...)
				},
			}).
			Build()
		_, stop, err := handleSecretRef(ctx, cl, key)
		require.ErrorContains(t, err, "cache not synced")
		assert.True(t, stop)
		assert.NotContains(t, secretRefValid(t, cl, key).Message, "required to recreate it")
	})

	t.Run("reports but doesn't require a missing key once created", func(t *testing.T) {
		key := newStaticKey(nil)
		key.SetKonnectID("static-key-id")
		cl := newClient(key, &corev1.Secret{Name: "key", Namespace: "default"})
		_, stop, err := handleSecretRef(ctx, cl, key)
		require.NoError(t, err)
		assert.False(t, stop)
		cond := secretRefValid(t, cl, key)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Contains(t, cond.Message, `missing key "key"`)
	})

	t.Run("reports a present Secret as valid once created", func(t *testing.T) {
		key := newStaticKey(nil)
		key.SetKonnectID("static-key-id")
		cl := newClient(key, &corev1.Secret{Name: "key", Namespace: "default", Data: map[string][]byte{"key": []byte("v")}})
		_, stop, err := handleSecretRef(ctx, cl, key)
		require.NoError(t, err)
		assert.False(t, stop)
		assert.Equal(t, metav1.ConditionTrue, secretRefValid(t, cl, key).Status)
	})

	t.Run("stops once created when no KongReferenceGrant allows the cross-namespace Secret", func(t *testing.T) {
		key := newStaticKey(new("other"))
		key.SetKonnectID("static-key-id")
		cl := newClient(key, &corev1.Secret{Name: "key", Namespace: "other", Data: map[string][]byte{"key": []byte("v")}})
		_, stop, err := handleSecretRef(ctx, cl, key)
		require.NoError(t, err)
		assert.True(t, stop)

		updated := &configurationv1alpha1.EventGatewayStaticKey{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(key), updated))
		cond, found := findConditionGeneric(updated.Status.Conditions, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs))
		require.True(t, found)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
	})
}
