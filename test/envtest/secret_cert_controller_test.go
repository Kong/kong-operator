package envtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	secretcert "github.com/kong/kong-operator/v2/controller/secret_cert"
	"github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestSecretCertReconciler(t *testing.T) {
	baseTLSSecret := func(namespace string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "secret-for-test",
				Namespace:    namespace,
				Labels:       map[string]string{},
				Annotations:  map[string]string{},
			},
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{
				"tls.crt": []byte("not-important-for-this-test"),
				"tls.key": []byte("not-important-for-this-test"),
			},
		}
	}
	expiredAnnotationValue := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name                   string
		adjust                 func(*corev1.Secret)
		expectDeletion         bool
		customExpirationMargin time.Duration
	}{
		{
			name: "actual requeue for short term valid DataPlane managed cert and delete it",
			adjust: func(s *corev1.Secret) {
				s.Annotations[consts.CertExpiresAtAnnotation] = time.Now().Add(20 * time.Second).UTC().Format(time.RFC3339)
				s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
				s.Labels[consts.GatewayOperatorManagedByLabel] = consts.DataPlaneManagedLabelValue
				s.Finalizers = []string{consts.DataPlaneOwnedWaitForOwnerFinalizer}
			},
			customExpirationMargin: 5 * time.Second,
			expectDeletion:         true,
		},
		{
			name: "expired cert with DataPlane managed is deleted",
			adjust: func(s *corev1.Secret) {
				s.Annotations[consts.CertExpiresAtAnnotation] = expiredAnnotationValue
				s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
				s.Labels[consts.GatewayOperatorManagedByLabel] = consts.DataPlaneManagedLabelValue
				s.Finalizers = []string{consts.DataPlaneOwnedWaitForOwnerFinalizer}

			},
			expectDeletion: false,
		},
		{
			name: "expired cert with ControlPlane managed is deleted",
			adjust: func(s *corev1.Secret) {
				s.Annotations[consts.CertExpiresAtAnnotation] = expiredAnnotationValue
				s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
				s.Labels[consts.GatewayOperatorManagedByLabel] = consts.ControlPlaneManagedLabelValue
			},
			expectDeletion: true,
		},
		{
			name: "valid cert secret is not deleted",
			adjust: func(s *corev1.Secret) {
				s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
				s.Labels[consts.GatewayOperatorManagedByLabel] = consts.ControlPlaneManagedLabelValue
			},
			expectDeletion: false,
		},
		{
			name: "secret without matching labels is ignored",
			adjust: func(s *corev1.Secret) {
				s.Labels = nil
			},
			expectDeletion: false,
		},
		{
			name: "non-TLS secret with matching labels is ignored",
			adjust: func(s *corev1.Secret) {
				s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
				s.Labels[consts.GatewayOperatorManagedByLabel] = consts.ControlPlaneManagedLabelValue
				s.Type = corev1.SecretTypeOpaque
				s.Data = map[string][]byte{"data": []byte("not-a-cert")}
			},
			expectDeletion: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
			mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

			reconciler := &secretcert.Reconciler{
				Client:      mgr.GetClient(),
				LoggingMode: logging.DevelopmentMode,
			}
			if tt.customExpirationMargin != 0 {
				reconciler.CertExpirationMargin = tt.customExpirationMargin
			}
			StartReconcilers(ctx, t, mgr, logs, reconciler)

			secret := baseTLSSecret(ns.Name)
			tt.adjust(secret)
			require.NoError(t, mgr.GetClient().Create(ctx, secret))

			nn := k8stypes.NamespacedName{Name: secret.Name, Namespace: ns.Name}
			if tt.expectDeletion {
				require.EventuallyWithT(t, func(ct *assert.CollectT) {
					s := &corev1.Secret{}
					err := mgr.GetClient().Get(ctx, nn, s)
					require.True(ct, apierrors.IsNotFound(err), "secret should be deleted, got: %v", err)
				}, 3*waitTime, tickTime, "secret should be deleted by the reconciler")
			} else {
				var existing corev1.Secret
				err := mgr.GetClient().Get(ctx, nn, &existing)
				require.NoError(t, err, "secret should not be deleted")
			}
		})
	}
}
