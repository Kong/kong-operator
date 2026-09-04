package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonconsts "github.com/kong/kong-operator/v2/api/common/consts"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

const (
	testCASecretName      = "test-ca"
	testCASecretNamespace = "test-ns"
	testDPName            = "my-dp"
)

// makeAIGWDP builds an AIGatewayDataPlane with an explicit UID so that ListSecretsForOwner
// can match OwnerReferences by UID.
func makeAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testCASecretNamespace,
			Name:      testDPName,
			UID:       types.UID("aigwdp-uid"),
		},
	}
}

// caSecret builds a Secret containing a self-signed RSA CA certificate.
func caSecret() *corev1.Secret {
	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("Kong Test CA"),
		certificate.WithCATrue(),
	)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCASecretNamespace, Name: testCASecretName},
		Data: map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		},
	}
}

func Test_ensureCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()

	tests := []struct {
		name              string
		reconciler        func() *Reconciler
		wantResult        op.Result
		wantErrContains   string
		wantConditionTrue bool
	}{
		{
			name: "CA exists: creates cert secret and sets CertificateProvisioned=True",
			reconciler: func() *Reconciler {
				aigwdp := makeAIGWDP()
				cl := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(aigwdp, caSecret()).
					Build()
				return &Reconciler{
					Client:                   cl,
					ClusterCASecretName:      testCASecretName,
					ClusterCASecretNamespace: testCASecretNamespace,
					CertTTL:                  consts.DefaultCertTTL,
				}
			},
			wantResult:        op.Created,
			wantConditionTrue: true,
		},
		{
			name: "CA secret missing: returns error and sets CertificateProvisioned=False",
			reconciler: func() *Reconciler {
				aigwdp := makeAIGWDP()
				cl := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(aigwdp).
					Build()
				return &Reconciler{
					Client:                   cl,
					ClusterCASecretName:      testCASecretName,
					ClusterCASecretNamespace: testCASecretNamespace,
				}
			},
			wantResult:        op.Noop,
			wantErrContains:   "not found",
			wantConditionTrue: false,
		},
		{
			name: "SecretLabelSelector adds extra label to matching labels",
			reconciler: func() *Reconciler {
				aigwdp := makeAIGWDP()
				cl := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(aigwdp, caSecret()).
					Build()
				return &Reconciler{
					Client:                   cl,
					ClusterCASecretName:      testCASecretName,
					ClusterCASecretNamespace: testCASecretNamespace,
					SecretLabelSelector:      "my-org/team",
					CertTTL:                  consts.DefaultCertTTL,
				}
			},
			wantResult:        op.Created,
			wantConditionTrue: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.reconciler()
			aigwdp := makeAIGWDP()

			res, secret, err := r.ensureCertificateSecret(context.Background(), aigwdp)

			assert.Equal(t, tc.wantResult, res)

			if tc.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContains)
				assert.Nil(t, secret)
			} else {
				require.NoError(t, err)
				require.NotNil(t, secret)
			}

			// Verify condition set on aigwdp.
			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
			require.NotNil(t, cond, "condition %q must be set", aigatewayv1alpha1.CertificateProvisionedType)
			if tc.wantConditionTrue {
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, string(aigatewayv1alpha1.CertificateProvisionedReason), cond.Reason)
				// Verify the returned secret has the expected standard labels.
				assert.Equal(t, "true", secret.Labels[consts.SecretAIGatewayDataPlaneCertificateLabel])
				// Verify TLS data is present.
				assert.Contains(t, secret.Data, "tls.crt")
				assert.Contains(t, secret.Data, "tls.key")
			} else {
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, string(aigatewayv1alpha1.UnableToProvisionReason), cond.Reason)
			}
		})
	}
}

const manualCertSecretName = "user-provided-cert"

// manualCertSecret builds a valid or invalid manually-referenced TLS Secret.
func manualCertSecret(valid bool) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: testCASecretNamespace, Name: manualCertSecretName},
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
	aigwdp := makeAIGWDP()
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
			r := &Reconciler{Client: cl}

			res, secret, err := r.getManualCertificateSecret(context.Background(), aigwdp)

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
}

func Test_getCertificateSecret_dispatch(t *testing.T) {
	scheme := managerscheme.Get()
	aigatewaycp := &konnectv1alpha1.KonnectAIGateway{}

	t.Run("Manual provisioning: fetches referenced secret, never calls EnsureCertificate", func(t *testing.T) {
		aigwdp := aigwdpWithManualCertRef()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(manualCertSecret(true)).Build()
		r := &Reconciler{Client: cl}

		res, secret, err := r.getCertificateSecret(context.Background(), aigwdp, aigatewaycp)

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		require.NotNil(t, secret)
		assert.Equal(t, manualCertSecretName, secret.Name)
	})

	t.Run("nil CertificateSecret: falls back to automatic provisioning", func(t *testing.T) {
		aigwdp := makeAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caSecret()).Build()
		r := &Reconciler{
			Client:                   cl,
			ClusterCASecretName:      testCASecretName,
			ClusterCASecretNamespace: testCASecretNamespace,
			CertTTL:                  consts.DefaultCertTTL,
		}

		res, secret, err := r.getCertificateSecret(context.Background(), aigwdp, aigatewaycp)

		require.NoError(t, err)
		assert.Equal(t, op.Created, res)
		require.NotNil(t, secret)
	})

	t.Run("no ControlPlaneRef, nothing configured: no certificate, no condition", func(t *testing.T) {
		aigwdp := makeAIGWDP()
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Reconciler{Client: cl}

		res, secret, err := r.getCertificateSecret(context.Background(), aigwdp, nil)

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		assert.Nil(t, apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType)))
	})

	t.Run("no ControlPlaneRef, Automatic requested anyway: no certificate, condition surfaces mismatch", func(t *testing.T) {
		aigwdp := makeAIGWDP()
		aigwdp.Spec.CertificateSecret = &aigatewayv1alpha1.CertificateSecret{
			Provisioning: new(aigatewayv1alpha1.AutomaticCertificateProvisioning),
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &Reconciler{Client: cl}

		res, secret, err := r.getCertificateSecret(context.Background(), aigwdp, nil)

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
		r := &Reconciler{Client: cl}

		res, secret, err := r.getCertificateSecret(context.Background(), aigwdp, nil)

		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Nil(t, secret)
		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.CertificateControlPlaneRefMissingReason), cond.Reason)
	})
}
