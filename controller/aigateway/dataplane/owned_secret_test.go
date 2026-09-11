package dataplane

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonconsts "github.com/kong/kong-operator/v2/api/common/consts"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// newTestAIGWDP builds an AIGatewayDataPlane with an explicit UID so owner
// references can be matched against it.
func newTestAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		Name:      "test-dp",
		Namespace: "default",
		UID:       types.UID("aigwdp-uid-123"),
	}
}

// managedCert builds an AIGatewayDataPlaneCertificate CR owned by aigwdp, so
// it's discoverable by cleanupStaleKonnectCertificates' List call regardless
// of whether it also carries the managed-by labels (older, pre-upgrade CRs
// may not).
func managedCert(name string, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		Name:      name,
		Namespace: aigwdp.Namespace,
		Labels:    selectorLabelsForAIGatewayDataPlane(aigwdp),
	}
	k8sutils.SetOwnerForObject(cert, aigwdp)
	return cert
}

// unlabeledManagedCert builds an AIGatewayDataPlaneCertificate CR owned by
// aigwdp but carrying no managed-by labels, mirroring certificates created
// before those labels existed.
func unlabeledManagedCert(name string, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	cert := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		Name:      name,
		Namespace: aigwdp.Namespace,
	}
	k8sutils.SetOwnerForObject(cert, aigwdp)
	return cert
}

func Test_cleanupStaleKonnectCertificates(t *testing.T) {
	aigwdp := newTestAIGWDP()

	t.Run("deletes every managed cert except the current one", func(t *testing.T) {
		current := managedCert("test-dp-current", aigwdp)
		stale1 := managedCert("test-dp-stale1", aigwdp)
		stale2 := managedCert("test-dp-stale2", aigwdp)
		// A cert belonging to a different AIGatewayDataPlane must never be touched.
		otherDPCert := managedCert("other-dp-current", &aigatewayv1alpha1.AIGatewayDataPlane{
			Name: "other-dp", Namespace: "default", UID: types.UID("other-dp-uid-456"),
		})

		cl := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithObjects(current, stale1, stale2, otherDPCert).
			Build()

		err := cleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), aigwdp, current.Name)
		require.NoError(t, err)

		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: current.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
			"current cert must survive")
		assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), types.NamespacedName{Name: stale1.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})),
			"stale1 must be deleted")
		assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), types.NamespacedName{Name: stale2.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})),
			"stale2 must be deleted")
		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: otherDPCert.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
			"cert belonging to a different AIGatewayDataPlane must not be touched")
	})

	t.Run("no-op when only the current cert exists", func(t *testing.T) {
		current := managedCert("test-dp-current", aigwdp)
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(current).Build()

		err := cleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), aigwdp, current.Name)
		require.NoError(t, err)

		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: current.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}))
	})

	t.Run("deletes a stale cert with no managed-by labels, as long as it's owned by aigwdp", func(t *testing.T) {
		// Mirrors a certificate created before the managed-by labels existed:
		// it must still be found and cleaned up via its owner reference.
		current := managedCert("test-dp-current", aigwdp)
		stalePreUpgrade := unlabeledManagedCert("test-dp", aigwdp)

		cl := fake.NewClientBuilder().
			WithScheme(managerscheme.Get()).
			WithObjects(current, stalePreUpgrade).
			Build()

		err := cleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), aigwdp, current.Name)
		require.NoError(t, err)

		assert.NoError(t, cl.Get(t.Context(), types.NamespacedName{Name: current.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}),
			"current cert must survive")
		assert.True(t, apierrors.IsNotFound(cl.Get(t.Context(), types.NamespacedName{Name: stalePreUpgrade.Name, Namespace: "default"}, &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{})),
			"unlabeled pre-upgrade cert must be deleted based on its owner reference")
	})

	t.Run("List error is propagated", func(t *testing.T) {
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return assert.AnError
			},
		})

		err := cleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), aigwdp, "test-dp-current")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list AIGatewayDataPlaneCertificates")
	})

	t.Run("non-NotFound Delete error is propagated", func(t *testing.T) {
		stale := managedCert("test-dp-stale", aigwdp)
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(stale).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return assert.AnError
			},
		})

		err := cleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), aigwdp, "test-dp-current")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete stale AIGatewayDataPlaneCertificate")
	})
}

// automaticProvisioningForTest returns a resolveAutomatic fallback for
// resolveCertificateSecret tests: it provisions an operator-managed
// certificate Secret exactly like the shared reconciler's default path does.
func automaticProvisioningForTest(
	cl client.Client,
) func(ctx context.Context, dp *aigatewayv1alpha1.AIGatewayDataPlane) (op.Result, *corev1.Secret, error) {
	return func(ctx context.Context, dp *aigatewayv1alpha1.AIGatewayDataPlane) (op.Result, *corev1.Secret, error) {
		return secrets.EnsureCertificate(
			ctx,
			dp,
			fmt.Sprintf("%s.%s", dp.Name, dp.Namespace),
			types.NamespacedName{Namespace: testCASecretNamespace, Name: testCASecretName},
			[]certificatesv1.KeyUsage{
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageClientAuth,
			},
			cl,
			client.MatchingLabels{
				pkgconsts.SecretProvisioningLabelKey:               pkgconsts.SecretProvisioningAutomaticLabelValue,
				pkgconsts.SecretAIGatewayDataPlaneCertificateLabel: "true",
			},
			pkgconsts.DefaultCertTTL,
		)
	}
}

// automaticFallbackUnexpected fails the test when the automatic-provisioning
// fallback is invoked where it must not be (Manual provisioning, or no
// ControlPlaneRef configured).
func automaticFallbackUnexpected(
	t *testing.T,
) func(ctx context.Context, dp *aigatewayv1alpha1.AIGatewayDataPlane) (op.Result, *corev1.Secret, error) {
	return func(context.Context, *aigatewayv1alpha1.AIGatewayDataPlane) (op.Result, *corev1.Secret, error) {
		t.Fatal("automatic certificate provisioning must not be invoked")
		return op.Noop, nil, nil
	}
}

const manualCertSecretName = "user-provided-cert"

// manualCertSecret builds a valid or invalid manually-referenced TLS Secret.
func manualCertSecret(valid bool) *corev1.Secret {
	s := &corev1.Secret{
		Namespace: testCASecretNamespace, Name: manualCertSecretName,
	}
	if !valid {
		s.Data = map[string][]byte{"tls.crt": []byte("not-a-cert")}
		return s
	}
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("user cert"))
	s.Data = map[string][]byte{"tls.crt": cert, "tls.key": key}
	return s
}

// aigwdpWithManualCertRef builds an AIGatewayDataPlane referencing
// manualCertSecretName via Manual provisioning.
func aigwdpWithManualCertRef() *aigatewayv1alpha1.AIGatewayDataPlane {
	aigwdp := newReconcileAIGWDP()
	aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
		Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
		SecretRef:    &aigatewayv1alpha1.SecretRef{Name: manualCertSecretName},
	}
	return aigwdp
}

func Test_getManualCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()

	tests := []struct {
		name            string
		objects         []client.Object
		wantResult      op.Result
		wantErrContains string
		wantSecretNil   bool
		wantCondStatus  metav1.ConditionStatus
		wantCondReason  commonconsts.ConditionReason
	}{
		{
			name:            "referenced secret not found",
			objects:         nil,
			wantResult:      op.Noop,
			wantErrContains: "not found",
			wantSecretNil:   true,
			wantCondStatus:  metav1.ConditionFalse,
			wantCondReason:  aigatewayv1alpha1.CertificateSecretRefNotFoundReason,
		},
		{
			name:           "referenced secret invalid: missing tls.key",
			objects:        []client.Object{manualCertSecret(false)},
			wantResult:     op.Noop,
			wantSecretNil:  true,
			wantCondStatus: metav1.ConditionFalse,
			wantCondReason: aigatewayv1alpha1.CertificateSecretInvalidReason,
		},
		{
			name:           "referenced secret valid",
			objects:        []client.Object{manualCertSecret(true)},
			wantResult:     op.Noop,
			wantSecretNil:  false,
			wantCondStatus: metav1.ConditionTrue,
			wantCondReason: aigatewayv1alpha1.CertificateProvisionedReason,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := aigwdpWithManualCertRef()
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()

			res, secret, err := getManualCertificateSecret(context.Background(), cl, aigwdp)

			assert.Equal(t, tc.wantResult, res)
			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
			} else {
				require.NoError(t, err)
			}
			if tc.wantSecretNil {
				assert.Nil(t, secret)
			} else {
				require.NotNil(t, secret)
				assert.Equal(t, manualCertSecretName, secret.Name)
			}

			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
			require.NotNil(t, cond)
			assert.Equal(t, tc.wantCondStatus, cond.Status)
			assert.Equal(t, string(tc.wantCondReason), cond.Reason)
		})
	}

	t.Run("transient Get error is not reported as a missing Secret", func(t *testing.T) {
		aigwdp := aigwdpWithManualCertRef()
		base := fake.NewClientBuilder().WithScheme(scheme).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return assert.AnError
			},
		})
		res, secret, err := getManualCertificateSecret(context.Background(), cl, aigwdp)

		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		require.Error(t, err)

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.UnableToProvisionReason), cond.Reason)
		assert.Contains(t, cond.Message, "failed to read certificate Secret")
	})
}

func Test_resolveCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()
	aigatewaycp := &konnectv1alpha1.KonnectAIGateway{}

	t.Run("Manual provisioning: fetches referenced secret, never calls EnsureCertificate", func(t *testing.T) {
		aigwdp := aigwdpWithManualCertRef()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(manualCertSecret(true)).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, aigatewaycp, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		require.NotNil(t, secret)
		assert.Equal(t, manualCertSecretName, secret.Name)
	})

	t.Run("nil CertificateSecret: falls back to automatic provisioning", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caSecret()).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, aigatewaycp, automaticProvisioningForTest(cl))

		require.NoError(t, err)
		assert.Equal(t, op.Created, res)
		require.NotNil(t, secret)
	})

	t.Run("no ControlPlaneRef, nothing configured: no certificate, no condition", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, nil, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))
	})

	t.Run("no ControlPlaneRef, Automatic requested anyway: no certificate, condition surfaces mismatch", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
			Provisioning: new(aigatewayv1alpha1.AutomaticCertificateProvisioning),
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, nil, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.CertificateControlPlaneRefMissingReason), cond.Reason)
	})

	t.Run("no ControlPlaneRef, Manual requested anyway: no lookup, condition surfaces mismatch", func(t *testing.T) {
		aigwdp := aigwdpWithManualCertRef()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(manualCertSecret(true)).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, nil, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.CertificateControlPlaneRefMissingReason), cond.Reason)
	})

	t.Run("no ControlPlaneRef, CertificateSecret cleared after being set: stale condition is removed", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
			Provisioning: new(aigatewayv1alpha1.AutomaticCertificateProvisioning),
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Prior reconcile: CertificateSecret was set, condition surfaces the mismatch.
		_, _, err := resolveCertificateSecret(context.Background(), cl, aigwdp, nil, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.NotNil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))

		// User clears CertificateSecret; still no ControlPlaneRef.
		aigwdp.Spec.CertificateSecret = nil
		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, nil, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))
	})
}

func Test_cleanupStaleAutomaticCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()

	t.Run("deletes the operator-provisioned Secret owned by the AIGatewayDataPlane", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(aigwdp, caSecret()).Build()
		_, automaticSecret, err := automaticProvisioningForTest(cl)(context.Background(), aigwdp)
		require.NoError(t, err)
		require.NotNil(t, automaticSecret)

		err = cleanupStaleAutomaticCertificateSecret(t.Context(), cl, logr.Discard(), aigwdp)
		require.NoError(t, err)

		err = cl.Get(t.Context(), types.NamespacedName{Namespace: automaticSecret.Namespace, Name: automaticSecret.Name}, &corev1.Secret{})
		assert.True(t, apierrors.IsNotFound(err), "automatic certificate Secret must be deleted")
	})

	t.Run("no automatic Secret present: no-op", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(aigwdp).Build()

		err := cleanupStaleAutomaticCertificateSecret(t.Context(), cl, logr.Discard(), aigwdp)
		require.NoError(t, err)
	})

	t.Run("List error is propagated", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(aigwdp).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return assert.AnError
			},
		})

		err := cleanupStaleAutomaticCertificateSecret(t.Context(), cl, logr.Discard(), aigwdp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list automatic certificate Secrets")
	})

	t.Run("non-NotFound Delete error is propagated", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(aigwdp, caSecret()).Build()
		_, automaticSecret, err := automaticProvisioningForTest(cl)(context.Background(), aigwdp)
		require.NoError(t, err)
		require.NotNil(t, automaticSecret)

		failingClient := interceptor.NewClient(cl, interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return assert.AnError
			},
		})

		err = cleanupStaleAutomaticCertificateSecret(t.Context(), failingClient, logr.Discard(), aigwdp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete stale automatic certificate Secret")
	})
}
