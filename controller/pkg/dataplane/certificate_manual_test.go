/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

const (
	manualTestNS         = "test-ns"
	manualTestSecretName = "user-cert"
	manualTestLabelKey   = "konghq.com/test-dp-cert"
)

var manualTestConditions = ManualCertificateConditions{
	Type:                         "CertificateProvisioned",
	ProvisionedReason:            "CertificateProvisioned",
	UnableToProvisionReason:      "UnableToProvision",
	SecretRefNotFoundReason:      "SecretRefNotFound",
	SecretRefNotFoundMessage:     func(name string) string { return "not found: " + name },
	SecretInvalidReason:          "InvalidSecret",
	SecretInvalidMessage:         "invalid",
	SecretOperatorManagedReason:  "SecretRefOperatorManaged",
	SecretOperatorManagedMessage: "operator managed",
}

func newManualTestDP() *eventgatewayv1alpha1.KegDataPlane {
	return &eventgatewayv1alpha1.KegDataPlane{
		Namespace:  manualTestNS,
		Name:       "my-dp",
		UID:        types.UID("dp-uid"),
		APIVersion: "eventgateway.konghq.com/v1alpha1",
		Kind:       "KegDataPlane",
	}
}

func tlsSecret(name string, crt, key []byte) *corev1.Secret {
	return &corev1.Secret{
		Namespace: manualTestNS, Name: name,
		Data: map[string][]byte{corev1.TLSCertKey: crt, corev1.TLSPrivateKeyKey: key},
	}
}

func TestCertificateChecksum(t *testing.T) {
	crt, key := certificate.MustGenerateCertPEMFormat()
	_, otherKey := certificate.MustGenerateCertPEMFormat()

	base := CertificateChecksum(tlsSecret("s", crt, key))
	assert.Len(t, base, 64)
	assert.Equal(t, base, CertificateChecksum(tlsSecret("other-name", crt, key)), "the Secret name must not affect the checksum")
	assert.NotEqual(t, base, CertificateChecksum(tlsSecret("s", crt, otherKey)), "a key-only rotation must change the checksum")
}

func TestCertEntityName(t *testing.T) {
	const checksum = "abcdef1234567890"

	assert.Equal(t, "my-dp-abcdef1234", CertEntityName("my-dp", checksum))

	long := strings.Repeat("a", 250)
	otherLong := strings.Repeat("a", 249) + "b"
	name := CertEntityName(long, checksum)
	assert.LessOrEqual(t, len(name), 253)
	assert.True(t, strings.HasSuffix(name, "-abcdef1234"))
	assert.NotEqual(t, name, CertEntityName(otherLong, checksum), "truncated names with a shared prefix must stay distinct")

	// 242 characters plus "-" and the 10-character suffix is exactly 253: no truncation.
	atLimit := strings.Repeat("b", 242)
	assert.Equal(t, atLimit+"-abcdef1234", CertEntityName(atLimit, checksum))
	assert.Len(t, CertEntityName(atLimit+"b", checksum), 253, "one character over the limit is truncated back to 253")

	// Truncation never leaves a trailing "." or "-" before the hash.
	dotted := strings.Repeat("c", 230) + strings.Repeat(".", 30)
	truncated := CertEntityName(dotted, checksum)
	assert.LessOrEqual(t, len(truncated), 253)
	assert.NotContains(t, truncated, ".-", "trailing dots must be trimmed before the hash suffix")
}

func TestValidateManualCertificate(t *testing.T) {
	now := time.Now()
	crt, key := certificate.MustGenerateCertPEMFormat()
	_, otherKey := certificate.MustGenerateCertPEMFormat()
	expiredCrt, expiredKey := certificate.MustGenerateCertPEMFormat(certificate.WithAlreadyExpired())

	tests := []struct {
		name        string
		secret      *corev1.Secret
		errContains string
	}{
		{name: "valid key pair", secret: tlsSecret("s", crt, key)},
		{
			name:        "missing tls.key",
			secret:      &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: crt}},
			errContains: "must both be present",
		},
		{
			name:        "not PEM",
			secret:      tlsSecret("s", []byte("not-a-cert"), []byte("not-a-key")),
			errContains: "must both be present",
		},
		{
			name:        "key does not match the certificate",
			secret:      tlsSecret("s", crt, otherKey),
			errContains: "do not form a valid key pair",
		},
		{
			name:        "expired certificate",
			secret:      tlsSecret("s", expiredCrt, expiredKey),
			errContains: "certificate expired at",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManualCertificate(tc.secret, now)
			if tc.errContains == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}

	t.Run("NotBefore within the clock skew allowance is accepted", func(t *testing.T) {
		crt, key := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(now.Add(time.Minute), now.Add(time.Hour)))
		require.NoError(t, validateManualCertificate(tlsSecret("s", crt, key), now))
	})

	t.Run("NotBefore beyond the clock skew allowance is rejected with a requeue", func(t *testing.T) {
		notBefore := now.Add(time.Hour)
		crt, key := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(notBefore, now.Add(2*time.Hour)))
		err := validateManualCertificate(tlsSecret("s", crt, key), now)

		var requeue *RequeueAfterError
		require.ErrorAs(t, err, &requeue)
		assert.Contains(t, err.Error(), "certificate not valid before")
		// Accepted once NotBefore is within the allowance; x509 truncates to seconds.
		assert.InDelta(t, notBefore.Add(-certificateClockSkewAllowance).Sub(now).Seconds(), requeue.After.Seconds(), 1)
	})

	t.Run("far-future NotBefore requeues after at most maxCertificateRequeue", func(t *testing.T) {
		crt, key := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(now.AddDate(70, 0, 0), now.AddDate(71, 0, 0)))
		var requeue *RequeueAfterError
		require.ErrorAs(t, validateManualCertificate(tlsSecret("s", crt, key), now), &requeue)
		assert.Equal(t, maxCertificateRequeue, requeue.After)
	})
}

func TestManualCertificateExpiryRequeue(t *testing.T) {
	now := time.Now()
	crt, key := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(now.Add(-time.Hour), now.Add(time.Hour)))
	expiredCrt, expiredKey := certificate.MustGenerateCertPEMFormat(certificate.WithAlreadyExpired())

	automatic := tlsSecret("s", crt, key)
	automatic.Labels = map[string]string{consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue}

	assert.InDelta(t, time.Hour.Seconds(), manualCertificateExpiryRequeue(tlsSecret("s", crt, key), now).Seconds(), 2,
		"a valid user-owned certificate is requeued just past its expiry")
	assert.Zero(t, manualCertificateExpiryRequeue(automatic, now), "operator-provisioned Secrets are never requeued")
	assert.Zero(t, manualCertificateExpiryRequeue(tlsSecret("s", expiredCrt, expiredKey), now), "already expired")
	assert.Zero(t, manualCertificateExpiryRequeue(tlsSecret("s", []byte("x"), []byte("y")), now), "unparsable")
	assert.Zero(t, manualCertificateExpiryRequeue(nil, now))

	longLivedCrt, longLivedKey := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(now.Add(-time.Hour), now.Add(48*time.Hour)))
	assert.Equal(t, maxCertificateRequeue, manualCertificateExpiryRequeue(tlsSecret("s", longLivedCrt, longLivedKey), now),
		"long-lived certificates are re-validated at most a day apart")
	noExpiryCrt, noExpiryKey := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(now.Add(-time.Hour), time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)))
	assert.Equal(t, maxCertificateRequeue, manualCertificateExpiryRequeue(tlsSecret("s", noExpiryCrt, noExpiryKey), now),
		"a 9999-12-31 NotAfter must not overflow into a negative delay")
}

func TestGetManualCertificateSecret(t *testing.T) {
	scheme := managerscheme.Get()
	crt, key := certificate.MustGenerateCertPEMFormat()
	_, otherKey := certificate.MustGenerateCertPEMFormat()

	operatorManaged := tlsSecret(manualTestSecretName, crt, key)
	operatorManaged.Labels = map[string]string{
		consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
	}

	tests := []struct {
		name          string
		objects       []client.Object
		wantSecret    bool
		wantStatus    metav1.ConditionStatus
		wantReason    string
		wantMsgSubstr string
	}{
		{
			name:          "missing Secret",
			wantStatus:    metav1.ConditionFalse,
			wantReason:    "SecretRefNotFound",
			wantMsgSubstr: "not found: " + manualTestSecretName,
		},
		{
			name:       "operator-provisioned Secret is rejected",
			objects:    []client.Object{operatorManaged},
			wantStatus: metav1.ConditionFalse,
			wantReason: "SecretRefOperatorManaged",
		},
		{
			name:          "mismatched key pair is rejected with the validation detail",
			objects:       []client.Object{tlsSecret(manualTestSecretName, crt, otherKey)},
			wantStatus:    metav1.ConditionFalse,
			wantReason:    "InvalidSecret",
			wantMsgSubstr: "invalid: tls.crt and tls.key do not form a valid key pair",
		},
		{
			name:       "valid Secret",
			objects:    []client.Object{tlsSecret(manualTestSecretName, crt, key)},
			wantSecret: true,
			wantStatus: metav1.ConditionTrue,
			wantReason: "CertificateProvisioned",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dp := newManualTestDP()
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()

			_, secret, err := GetManualCertificateSecret(t.Context(), cl, dp, manualTestSecretName, manualTestConditions)
			require.NoError(t, err)
			assert.Equal(t, tc.wantSecret, secret != nil)

			cond := apimeta.FindStatusCondition(dp.Status.Conditions, manualTestConditions.Type)
			require.NotNil(t, cond)
			assert.Equal(t, tc.wantStatus, cond.Status)
			assert.Equal(t, tc.wantReason, cond.Reason)
			assert.Contains(t, cond.Message, tc.wantMsgSubstr)
		})
	}

	t.Run("not yet valid certificate: InvalidSecret and a requeue for when it becomes valid", func(t *testing.T) {
		dp := newManualTestDP()
		notYetCrt, notYetKey := certificate.MustGenerateCertPEMFormat(certificate.WithValidity(time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)))
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tlsSecret(manualTestSecretName, notYetCrt, notYetKey)).Build()

		_, secret, err := GetManualCertificateSecret(t.Context(), cl, dp, manualTestSecretName, manualTestConditions)
		var requeue *RequeueAfterError
		require.ErrorAs(t, err, &requeue)
		assert.Positive(t, requeue.After)
		assert.Nil(t, secret)

		cond := apimeta.FindStatusCondition(dp.Status.Conditions, manualTestConditions.Type)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "InvalidSecret", cond.Reason)
		assert.Contains(t, cond.Message, "certificate not valid before")
	})

	t.Run("transient Get error is returned and not reported as a missing Secret", func(t *testing.T) {
		dp := newManualTestDP()
		cl := interceptor.NewClient(fake.NewClientBuilder().WithScheme(scheme).Build(), interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return assert.AnError
			},
		})

		_, secret, err := GetManualCertificateSecret(t.Context(), cl, dp, manualTestSecretName, manualTestConditions)
		require.ErrorIs(t, err, assert.AnError)
		assert.Nil(t, secret)

		cond := apimeta.FindStatusCondition(dp.Status.Conditions, manualTestConditions.Type)
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "UnableToProvision", cond.Reason)
	})
}

// failingClient returns a client whose List or Delete calls fail with
// assert.AnError.
func failingClient(failList, failDelete bool, objs ...client.Object) client.Client {
	return interceptor.NewClient(fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build(), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if failList {
				return assert.AnError
			}
			return c.List(ctx, list, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if failDelete {
				return assert.AnError
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
}

func TestCleanupStaleAutomaticCertificateSecret(t *testing.T) {
	dp := newManualTestDP()
	other := newManualTestDP()
	other.Name, other.UID = "other-dp", "other-uid"

	automatic := func(name string, owner *eventgatewayv1alpha1.KegDataPlane, labelKey string) *corev1.Secret {
		s := &corev1.Secret{
			Namespace: manualTestNS, Name: name,
			Labels: map[string]string{
				consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
				labelKey:                          "true",
			},
		}
		k8sutils.SetOwnerForObject(s, owner)
		return s
	}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(
		automatic("stale", dp, manualTestLabelKey),
		automatic("other-owner", other, manualTestLabelKey),
		automatic("other-label", dp, "konghq.com/other-cert"),
		tlsSecret(manualTestSecretName, nil, nil),
	).Build()

	require.NoError(t, CleanupStaleAutomaticCertificateSecret(t.Context(), cl, logr.Discard(), dp, manualTestLabelKey))

	exists := func(name string) bool {
		err := cl.Get(t.Context(), types.NamespacedName{Namespace: manualTestNS, Name: name}, &corev1.Secret{})
		if err != nil {
			require.True(t, apierrors.IsNotFound(err))
		}
		return err == nil
	}
	assert.False(t, exists("stale"))
	assert.True(t, exists("other-owner"), "Secrets owned by another DataPlane must be kept")
	assert.True(t, exists("other-label"), "Secrets marked with another certificate label must be kept")
	assert.True(t, exists(manualTestSecretName), "user-owned Secrets must never be touched")

	t.Run("List error is propagated", func(t *testing.T) {
		err := CleanupStaleAutomaticCertificateSecret(t.Context(), failingClient(true, false), logr.Discard(), dp, manualTestLabelKey)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to list automatic certificate Secrets")
	})

	t.Run("non-NotFound Delete error is propagated", func(t *testing.T) {
		cl := failingClient(false, true, automatic("stale", dp, manualTestLabelKey))
		err := CleanupStaleAutomaticCertificateSecret(t.Context(), cl, logr.Discard(), dp, manualTestLabelKey)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to delete stale automatic certificate Secret")
	})
}

func TestCleanupStaleKonnectCertificates(t *testing.T) {
	dp := newManualTestDP()
	other := newManualTestDP()
	other.Name, other.UID = "other-dp", "other-uid"

	cert := func(name string, owner *eventgatewayv1alpha1.KegDataPlane) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		c := &configurationv1alpha1.EventGatewayDataPlaneCertificate{Namespace: manualTestNS, Name: name}
		k8sutils.SetOwnerForObject(c, owner)
		return c
	}
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(
		cert("current", dp),
		cert("stale", dp),
		cert("other-owner", other),
	).Build()

	require.NoError(t, CleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), dp,
		&configurationv1alpha1.EventGatewayDataPlaneCertificateList{}, "EventGatewayDataPlaneCertificate", "current"))

	exists := func(name string) bool {
		err := cl.Get(t.Context(), types.NamespacedName{Namespace: manualTestNS, Name: name}, &configurationv1alpha1.EventGatewayDataPlaneCertificate{})
		if err != nil {
			require.True(t, apierrors.IsNotFound(err))
		}
		return err == nil
	}
	assert.True(t, exists("current"))
	assert.False(t, exists("stale"))
	assert.True(t, exists("other-owner"), "certificates owned by another DataPlane must be kept")

	t.Run("List error is propagated", func(t *testing.T) {
		err := CleanupStaleKonnectCertificates(t.Context(), failingClient(true, false), logr.Discard(), dp,
			&configurationv1alpha1.EventGatewayDataPlaneCertificateList{}, "EventGatewayDataPlaneCertificate", "current")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to list EventGatewayDataPlaneCertificates")
	})

	t.Run("non-NotFound Delete error is propagated", func(t *testing.T) {
		cl := failingClient(false, true, cert("stale", dp))
		err := CleanupStaleKonnectCertificates(t.Context(), cl, logr.Discard(), dp,
			&configurationv1alpha1.EventGatewayDataPlaneCertificateList{}, "EventGatewayDataPlaneCertificate", "current")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to delete stale EventGatewayDataPlaneCertificate")
	})
}
