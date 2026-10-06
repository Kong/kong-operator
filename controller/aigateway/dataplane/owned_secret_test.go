package dataplane

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	pkgconsts "github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

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

// resolvedKonnectAIGatewayCP wraps a KonnectAIGateway into the
// shareddataplane.ResolvedControlPlane expected by resolveCertificateSecret.
// A nil control plane returns the zero ResolvedControlPlane: wrapping a typed
// nil pointer would produce a non-nil Object interface and misrepresent an
// unconfigured control plane as configured.
func resolvedKonnectAIGatewayCP(cp *konnectv1alpha1.KonnectAIGateway) shareddataplane.ResolvedControlPlane {
	if cp == nil {
		return shareddataplane.ResolvedControlPlane{}
	}
	return shareddataplane.ResolvedControlPlane{
		Kind:      "KonnectAIGateway",
		IsKonnect: true,
		Object:    cp,
	}
}

func Test_resolveCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()
	aigatewaycp := &konnectv1alpha1.KonnectAIGateway{}
	t.Run("Manual provisioning: fetches referenced secret, never calls EnsureCertificate", func(t *testing.T) {
		aigwdp := aigwdpWithManualCertRef()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(manualCertSecret(true)).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, resolvedKonnectAIGatewayCP(aigatewaycp), automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		require.NotNil(t, secret)
		assert.Equal(t, manualCertSecretName, secret.Name)
	})

	t.Run("nil CertificateSecret: falls back to automatic provisioning", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caSecret()).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, resolvedKonnectAIGatewayCP(aigatewaycp), automaticProvisioningForTest(cl))

		require.NoError(t, err)
		assert.Equal(t, op.Created, res)
		require.NotNil(t, secret)
	})

	t.Run("no ControlPlaneRef, nothing configured: no certificate, no condition", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))

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

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))

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

		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))

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
		_, _, err := resolveCertificateSecret(context.Background(), cl, aigwdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))
		require.NoError(t, err)
		require.NotNil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))

		// User clears CertificateSecret; still no ControlPlaneRef.
		aigwdp.Spec.CertificateSecret = nil
		res, secret, err := resolveCertificateSecret(context.Background(), cl, aigwdp, shareddataplane.ResolvedControlPlane{}, automaticFallbackUnexpected(t))

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))
	})
}
