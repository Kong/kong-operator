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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
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
