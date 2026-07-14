package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name: "test-secret",
					},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "cert-ns",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "cert-ns",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "cert-ns",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
				Spec: configurationv1alpha1.KongCertificateSpec{
					SecretRef: &commonv1alpha1.NamespacedRef{
						Name:      "test-secret",
						Namespace: new("secret-ns"),
					},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "cert-ns",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-alt",
						Namespace: "secret-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secrets",
						Namespace: "secret-ns",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "cert-ns",
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongCertificate",
				},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-alt",
						Namespace: "secret-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-cert",
						Namespace:         "default",
						DeletionTimestamp: &now,
						Finalizers:        []string{KonnectCleanupFinalizer},
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongCertificate",
					},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-cert",
						Namespace:         "cert-ns",
						DeletionTimestamp: &now,
						Finalizers:        []string{KonnectCleanupFinalizer},
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongCertificate",
					},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "secret-ns",
					},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cert",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "KongCertificate",
		},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
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

func findCondition(cert *configurationv1alpha1.KongCertificate, condType string) (metav1.Condition, bool) {
	for _, cond := range cert.Status.Conditions {
		if cond.Type == condType {
			return cond, true
		}
	}
	return metav1.Condition{}, false
}
