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
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretcert "github.com/kong/kong-operator/v2/controller/secret_cert"
	"github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func baseTLSSecret(namespace string) *corev1.Secret {
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

type secretCertReconcilerTestCase struct {
	name                   string
	adjust                 func(*corev1.Secret)
	expectDeletion         bool
	customExpirationMargin time.Duration
}

func TestSecretCertReconciler(t *testing.T) {
	t.Parallel()

	expiredAnnotationValue := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	tests := []secretCertReconcilerTestCase{
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

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	reconciler := &secretcert.Reconciler{
		Client:      mgr.GetClient(),
		LoggingMode: logging.DevelopmentMode,
	}
	StartReconcilers(ctx, t, mgr, logs, reconciler)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secretCertReconcilerTest(t, ns, mgr.GetClient(), tt)
		})
	}
}

func TestSecretCertReconcilerCustomMargin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	reconciler := &secretcert.Reconciler{
		Client:      mgr.GetClient(),
		LoggingMode: logging.DevelopmentMode,
	}
	StartReconcilers(ctx, t, mgr, logs, reconciler)

	tc := secretCertReconcilerTestCase{
		name: "actual requeue for short term valid DataPlane managed cert and delete it",
		adjust: func(s *corev1.Secret) {
			s.Annotations[consts.CertExpiresAtAnnotation] = time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
			s.Labels[config.DefaultSecretLabelSelector] = config.LabelValueForSelectorTrue
			s.Labels[consts.GatewayOperatorManagedByLabel] = consts.DataPlaneManagedLabelValue
			s.Finalizers = []string{consts.DataPlaneOwnedWaitForOwnerFinalizer}
		},
		customExpirationMargin: 2 * time.Second,
		expectDeletion:         true,
	}
	secretCertReconcilerTest(t, ns, mgr.GetClient(), tc)
}

func secretCertReconcilerTest(
	t *testing.T,
	ns *corev1.Namespace,
	cl client.Client,
	tt secretCertReconcilerTestCase,
) {
	t.Helper()
	ctx := t.Context()

	secret := baseTLSSecret(ns.Name)
	tt.adjust(secret)
	require.NoError(t, cl.Create(ctx, secret))

	nn := k8stypes.NamespacedName{Name: secret.Name, Namespace: ns.Name}
	if tt.expectDeletion {
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := cl.Get(ctx, nn, &corev1.Secret{})
			require.True(t, apierrors.IsNotFound(err), "secret should be deleted, got: %v", err)
		}, 3*waitTime, tickTime, "secret should be deleted by the reconciler")
		return
	}

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := cl.Get(ctx, nn, &corev1.Secret{})
		require.NoError(t, err, "secret should not be deleted, got: %v", err)
	}, waitTime, tickTime, "secret should not be deleted by the reconciler")
}
