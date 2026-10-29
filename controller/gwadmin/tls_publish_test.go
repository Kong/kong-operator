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

package gwadmin

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// cDataPlaneCertificate is a client certificate a data plane provisioner
// issued from the published cp client CA.
type cDataPlaneCertificate struct {
	secret *corev1.Secret
	issuer *x509.Certificate
}

// cIssueDataPlaneCertificate issues a data plane client certificate from the
// published cp client CA, as the controller managing the data planes
// would.
func cIssueDataPlaneCertificate(t *testing.T, r *Reconciler, now time.Time) cDataPlaneCertificate {
	t.Helper()
	published := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")
	issuer := caCert(t, published)
	certPEM, keyPEM, err := issueCertificate(published, certRequest{
		commonName: "data-plane", usages: operatorClientLeafSpec().usages,
		ttl: 30 * 24 * time.Hour, now: now,
	})
	require.NoError(t, err)
	return cDataPlaneCertificate{
		secret: &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}},
		issuer: issuer,
	}
}

// cAcceptedBy reports whether pods running the material m accept the data
// plane certificate at now.
func cAcceptedBy(m *tlsMaterial, dp cDataPlaneCertificate, now time.Time) bool {
	roots := x509.NewCertPool()
	for _, c := range clientBundleCerts(m) {
		roots.AddCert(c)
	}
	_, err := leafCertificate(dp.secret).Verify(x509.VerifyOptions{
		Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, CurrentTime: now,
	})
	return err == nil
}

// cExpiredCA returns a certificate authority that expired a day ago.
func cExpiredCA(t *testing.T, now time.Time) *x509.Certificate {
	t.Helper()
	return caCert(t, userCASecretValidAt(t, "expired", now.Add(-48*time.Hour), time.Hour))
}

// cAppendToBundle appends cert to the ca.crt of the named bundle Secret.
func cAppendToBundle(t *testing.T, r *Reconciler, name string, cert *x509.Certificate) {
	t.Helper()
	live := getSecret(t, r, name)
	live.Data[consts.CACRT] = append(live.Data[consts.CACRT], encodeCertificates([]*x509.Certificate{cert})...)
	require.NoError(t, r.Update(t.Context(), live))
}

// cDataPlane is the name of the data plane Deployment the tests use.
const (
	cDataPlaneNamespace = "dp-ns"
	cDataPlaneName      = "dp"
)

// cSettle reconciles the GWAdmin a few times, rolling the gw-admin pods and
// the data plane after every pass, as the controllers managing them would,
// and returns the material the pods run in the end.
func cSettle(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin, m *tlsMaterial) *tlsMaterial {
	t.Helper()
	for range 6 {
		m = ensure(t, r, gw, rolledDeployment(m))
		rollDataPlane(t, r, cDataPlaneNamespace, cDataPlaneName, m)
	}
	return ensure(t, r, gw, rolledDeployment(m))
}

// cHasPrevious reports whether the published Secret records certificate
// authorities published before it.
func cHasPrevious(t *testing.T, r *Reconciler) bool {
	t.Helper()
	return len(getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data[gwadminref.PreviousCAKey]) > 0
}

// TestEnsureTLS_ClientDataPlaneCertificatesSurviveRotations follows
// rotations of the cp client CA pass by pass, a data plane certificate
// being issued from the published CA after every pass. The pods accept every
// one of them, whether they still run the previous material or the new one,
// until the data plane rolls out with a certificate from the new CA.
func TestEnsureTLS_ClientDataPlaneCertificatesSurviveRotations(t *testing.T) {
	manual := func() *operatorv1alpha1.GWAdminTLS {
		return &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}
	}
	automatic := func() *operatorv1alpha1.GWAdminTLS { return &operatorv1alpha1.GWAdminTLS{} }

	for _, tc := range []struct {
		name   string
		tls    *operatorv1alpha1.GWAdminTLS
		rotate func(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin, now *time.Time)
	}{
		{
			name: "automatic renewal",
			tls:  automatic(),
			rotate: func(t *testing.T, r *Reconciler, _ *operatorv1alpha1.GWAdmin, now *time.Time) {
				*now = renewalTime(publishedClientCA(t, r))
			},
		},
		{
			name: "manual CA changed in place",
			tls:  manual(),
			rotate: func(t *testing.T, r *Reconciler, _ *operatorv1alpha1.GWAdmin, _ *time.Time) {
				rotateManualCA(t, r, "ca-b")
			},
		},
		{
			name: "manual CA changed in place, with a manual cp certificate",
			tls: func() *operatorv1alpha1.GWAdminTLS {
				tls := manual()
				tls.CP = manualCP()
				return tls
			}(),
			rotate: func(t *testing.T, r *Reconciler, _ *operatorv1alpha1.GWAdmin, _ *time.Time) {
				rotateManualCA(t, r, "ca-b")
			},
		},
		{
			name: "automatic to manual",
			tls:  automatic(),
			rotate: func(_ *testing.T, _ *Reconciler, gw *operatorv1alpha1.GWAdmin, _ *time.Time) {
				gw.Spec.TLS.ClientCA = manualCA(userCASecretName)
			},
		},
		{
			name: "manual to automatic",
			tls:  manual(),
			rotate: func(_ *testing.T, _ *Reconciler, gw *operatorv1alpha1.GWAdmin, _ *time.Time) {
				gw.Spec.TLS.ClientCA = nil
			},
		},
		{
			name: "rotation annotation",
			tls:  automatic(),
			rotate: func(_ *testing.T, _ *Reconciler, gw *operatorv1alpha1.GWAdmin, _ *time.Time) {
				gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(tc.tls)
			src, _ := userCPSecret(t, time.Time{}, false)
			r := newReconciler(t, &now, gw, userCASecretValidAt(t, "user", now, 2*consts.GWAdminCATTL), src)
			running := ensure(t, r, gw, nil)
			running = cSettle(t, r, gw, running)
			require.False(t, running.rotating, certificateCondition(gw).Message)
			previous := publishedClientCA(t, r)

			tc.rotate(t, r, gw, &now)
			issued := []cDataPlaneCertificate{cIssueDataPlaneCertificate(t, r, now)}
			for pass := range 8 {
				m := ensure(t, r, gw, rolledDeployment(running))
				require.LessOrEqual(t, len(clientBundleCerts(m)), 2, "pass %d", pass)
				issued = append(issued, cIssueDataPlaneCertificate(t, r, now))
				// The pods still run the previous material until they
				// roll, and the new one afterwards: both accept every data
				// plane certificate issued so far.
				for i, dp := range issued {
					require.True(t, cAcceptedBy(running, dp, now), "pass %d: certificate %d rejected by the running pods", pass, i)
					require.True(t, cAcceptedBy(m, dp, now), "pass %d: certificate %d rejected by the new pods", pass, i)
				}
				running = m
			}

			// Settled, but the data plane still runs a certificate from the
			// previous CA: it stays trusted, however long that takes.
			current := publishedClientCA(t, r)
			require.False(t, current.Equal(previous), "a new certificate authority is published")
			require.Len(t, clientBundleCerts(running), 2)
			assert.True(t, running.rotating)
			assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp-ns/dp")
			assert.True(t, cHasPrevious(t, r), "the published Secret records the previous CA")
			// Time alone never drops the previous CA: 20h later, within the
			// 24h the Manual cp certificate is valid for, it is still trusted.
			now = now.Add(20 * time.Hour)
			running = ensure(t, r, gw, rolledDeployment(running))
			require.Len(t, clientBundleCerts(running), 2, "no time limit")

			// The data plane rolls out with a certificate from the new CA:
			// the previous one is dropped, and forgotten.
			running = cSettle(t, r, gw, running)
			require.Len(t, clientBundleCerts(running), 1)
			assert.True(t, clientBundleCerts(running)[0].Equal(current))
			assert.False(t, running.rotating, certificateCondition(gw).Message)
			assert.False(t, cHasPrevious(t, r))
			assert.True(t, cAcceptedBy(running, cIssueDataPlaneCertificate(t, r, now), now))
			for i, dp := range issued {
				if dp.issuer.Equal(previous) {
					assert.False(t, cAcceptedBy(running, dp, dp.secretNotBefore(t)), "certificate %d", i)
				}
			}
		})
	}
}

// secretNotBefore returns a moment the certificate is valid at.
func (c cDataPlaneCertificate) secretNotBefore(t *testing.T) time.Time {
	t.Helper()
	leaf := leafCertificate(c.secret)
	require.NotNil(t, leaf)
	return leaf.NotBefore.Add(time.Minute)
}

// TestEnsureTLS_ClientWithoutDataPlanes checks that without any data plane,
// the previous cp client CA is dropped as soon as the new one is published:
// nothing can hold a certificate from it.
func TestEnsureTLS_ClientWithoutDataPlanes(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))
	certB := rotateManualCA(t, r, "ca-b")

	m1 := ensure(t, r, gw, rolledDeployment(m)) // B trusted.
	require.Len(t, clientBundleCerts(m1), 2)
	m2 := ensure(t, r, gw, rolledDeployment(m1)) // B published, A dropped.
	require.True(t, publishedClientCA(t, r).Equal(certB))
	require.Len(t, clientBundleCerts(m2), 1)
	assert.True(t, clientBundleCerts(m2)[0].Equal(certB))
	assert.False(t, cHasPrevious(t, r))
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	assert.False(t, m3.rotating, certificateCondition(gw).Message)
}

// TestEnsureTLS_ClientWaitsForTheDataPlanes checks the condition names the
// data plane that doesn't run a certificate from the published CA yet, and
// that a data plane rolling out keeps the rotation waiting.
func TestEnsureTLS_ClientWaitsForTheDataPlanes(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
	m := ensure(t, r, gw, nil)
	m = cSettle(t, r, gw, m)
	rotateManualCA(t, r, "ca-b")
	for range 3 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}

	cond := certificateCondition(gw)
	require.NotNil(t, cond)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason)
	assert.Contains(t, cond.Message, "stays trusted until every data plane runs a certificate issued from the new one")
	assert.Contains(t, cond.Message, "data plane Deployment dp-ns/dp doesn't run it yet")
	assert.Equal(t, pki.MaxRenewalRequeue, m.requeueAfter, "the data plane rollout triggers the reconcile, not time")

	// The data plane template moved, but its rollout isn't complete.
	rollDataPlane(t, r, cDataPlaneNamespace, cDataPlaneName, m)
	live := &appsv1.Deployment{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Namespace: cDataPlaneNamespace, Name: cDataPlaneName}, live))
	live.Status.UpdatedReplicas = 0
	// Deployments have a status subresource in the fake client.
	require.NoError(t, r.Status().Update(t.Context(), live))
	m = ensure(t, r, gw, rolledDeployment(m))
	require.Len(t, clientBundleCerts(m), 2)
	assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp-ns/dp: waiting for the Deployment to roll out")

	// The rollout completes. rollDataPlane writes the status with an
	// Update, which the status subresource ignores.
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Namespace: cDataPlaneNamespace, Name: cDataPlaneName}, live))
	live.Status.UpdatedReplicas = 1
	require.NoError(t, r.Status().Update(t.Context(), live))
	m = cSettle(t, r, gw, m)
	require.Len(t, clientBundleCerts(m), 1)
}

// TestEnsureTLS_ClientPreviousSurvivesALostBundle checks that the client
// bundle is derived: when it is lost while a data plane still runs a
// certificate from the previous CA, it is rebuilt with it, from the
// published Secret, and without it once the data plane moved.
func TestEnsureTLS_ClientPreviousSurvivesALostBundle(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
	m := ensure(t, r, gw, nil)
	m = cSettle(t, r, gw, m)
	certA := publishedClientCA(t, r)
	certB := rotateManualCA(t, r, "ca-b")
	for range 4 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}
	require.True(t, publishedClientCA(t, r).Equal(certB))
	require.Len(t, clientBundleCerts(m), 2)

	require.NoError(t, r.Delete(t.Context(), getSecret(t, r, "gw-cp-client-ca-bundle")))
	m = ensure(t, r, gw, rolledDeployment(m))
	require.Len(t, clientBundleCerts(m), 2, "A is trusted again until the data plane moves")
	assert.True(t, containsCertificate(clientBundleCerts(m), certA))
	assert.True(t, containsCertificate(clientBundleCerts(m), certB))

	m = cSettle(t, r, gw, m)
	require.Len(t, clientBundleCerts(m), 1)
	require.NoError(t, r.Delete(t.Context(), getSecret(t, r, "gw-cp-client-ca-bundle")))
	m = ensure(t, r, gw, rolledDeployment(m))
	require.Len(t, clientBundleCerts(m), 1, "not once it moved")
	assert.True(t, clientBundleCerts(m)[0].Equal(certB))
}

// TestEnsureTLS_ClientPreviousSurvivesALostPublishedSecret checks that the
// previous cp client CA stays trusted until the data plane moved when the
// Secret it was published in is deleted, which also loses the previous ones
// it recorded.
func TestEnsureTLS_ClientPreviousSurvivesALostPublishedSecret(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	m = cSettle(t, r, gw, m)
	dp := cIssueDataPlaneCertificate(t, r, now)

	require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")))
	// A new cp client CA is created, trusted, and published once the pods
	// trust it: the data plane certificate stays accepted all along.
	for range 5 {
		m = ensure(t, r, gw, rolledDeployment(m))
		require.True(t, cAcceptedBy(m, dp, now))
	}
	require.False(t, publishedClientCA(t, r).Equal(dp.issuer), "a new cp client CA is published")
	require.Len(t, clientBundleCerts(m), 2)

	m = cSettle(t, r, gw, m)
	assert.False(t, cAcceptedBy(m, dp, now), "the data plane moved")
	assert.Len(t, clientBundleCerts(m), 1)
}

// TestEnsureTLS_ClientSeveralRotationsBeforeTheDataPlanesMove checks that
// every cp client CA published since the one the data plane runs stays
// trusted, not only the last one.
func TestEnsureTLS_ClientSeveralRotationsBeforeTheDataPlanesMove(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	m = cSettle(t, r, gw, m)

	rotate := func(token string) cDataPlaneCertificate {
		dp := cIssueDataPlaneCertificate(t, r, now)
		gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: token}
		for range 4 {
			m = ensure(t, r, gw, rolledDeployment(m))
		}
		require.False(t, publishedClientCA(t, r).Equal(dp.issuer), "rotated")
		return dp
	}
	first := rotate("1")
	second := rotate("2")
	third := cIssueDataPlaneCertificate(t, r, now)
	for i, dp := range []cDataPlaneCertificate{first, second, third} {
		assert.True(t, cAcceptedBy(m, dp, now), "certificate %d", i)
	}
	assert.Len(t, clientBundleCerts(m), 3)

	m = cSettle(t, r, gw, m)
	assert.False(t, cAcceptedBy(m, first, now))
	assert.False(t, cAcceptedBy(m, second, now))
	assert.True(t, cAcceptedBy(m, third, now))
	assert.Len(t, clientBundleCerts(m), 1)
}

// TestEnsureTLS_ClientNewDataPlaneDoesNotBringBackAnOldCA checks that once
// every data plane moved and the previous CA was forgotten, a new data plane
// that didn't roll out yet doesn't make Kong Operator trust it again.
func TestEnsureTLS_ClientNewDataPlaneDoesNotBringBackAnOldCA(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
	m := ensure(t, r, gw, nil)
	m = cSettle(t, r, gw, m)
	certA := publishedClientCA(t, r)
	certB := rotateManualCA(t, r, "ca-b")
	m = cSettle(t, r, gw, m)
	require.Len(t, clientBundleCerts(m), 1)
	require.True(t, clientBundleCerts(m)[0].Equal(certB))

	grantDataPlanes(t, r, "other-ns")
	require.NoError(t, r.Create(t.Context(), dataPlaneDeploymentWith("other-ns", "new-dp",
		gwadminref.DataPlaneServerBundleChecksum(m.serverBundle), gwadminref.DataPlaneClientCAFingerprint(encodeCertificates([]*x509.Certificate{certA})), false)))
	for range 3 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}
	require.Len(t, clientBundleCerts(m), 1)
	assert.False(t, containsCertificate(clientBundleCerts(m), certA))
	assert.False(t, m.rotating, certificateCondition(gw).Message)
}

// TestEnsureTLS_ClientBundleNeverAccumulates checks that a Manual cp client
// CA changed several times while the rollout never completes leaves the
// client bundle with the signer and the published CA only: a CA that was
// never published is dropped as soon as it is replaced.
func TestEnsureTLS_ClientBundleNeverAccumulates(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))
	certA := publishedClientCA(t, r)

	var replaced []*x509.Certificate
	for _, cn := range []string{"ca-b", "ca-c", "ca-d", "ca-e"} {
		signer := rotateManualCA(t, r, cn)
		got := ensure(t, r, gw, rollingDeployment(m))
		certs := clientBundleCerts(got)
		require.Len(t, certs, 2, cn)
		assert.True(t, containsCertificate(certs, signer), cn)
		assert.True(t, containsCertificate(certs, certA), "%s: the published CA stays trusted", cn)
		for _, old := range replaced {
			assert.False(t, containsCertificate(certs, old), "%s: a CA never published is dropped", cn)
		}
		assert.True(t, publishedClientCA(t, r).Equal(certA), "nothing is published before the pods trust it")
		replaced = append(replaced, signer)
	}
}

// TestEnsureTLS_ClientExpiredCAsLeaveEveryBundle checks that an expired
// certificate authority is dropped from every bundle.
func TestEnsureTLS_ClientExpiredCAsLeaveEveryBundle(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))

	expired := cExpiredCA(t, now)
	for _, name := range []string{"gw-api-ca-bundle", "gw-cp-client-ca-bundle", "gw-cp-server-ca-bundle"} {
		cAppendToBundle(t, r, name, expired)
	}
	for range 3 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}
	for name, certs := range map[string][]*x509.Certificate{
		"api": apiBundleCerts(m), "client": clientBundleCerts(m), "server": serverBundleCerts(m),
	} {
		assert.False(t, containsCertificate(certs, expired), name)
		assert.Len(t, certs, 1, name)
	}
}

// TestEnsureTLS_ClientExpiredPublishedCAIsDropped checks that a published
// Manual CA that expires while the next one waits for a rollout that never
// completes leaves the client bundle.
func TestEnsureTLS_ClientExpiredPublishedCAIsDropped(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecretValidAt(t, "ca-a", now, 2*time.Hour))
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))
	certA := publishedClientCA(t, r)
	signer := rotateManualCA(t, r, "ca-b")

	got := ensure(t, r, gw, rollingDeployment(m))
	require.Len(t, clientBundleCerts(got), 2)

	now = certA.NotAfter
	got = ensure(t, r, gw, rollingDeployment(m))
	require.Len(t, clientBundleCerts(got), 1)
	assert.True(t, clientBundleCerts(got)[0].Equal(signer))
}

// cCASecrets returns the certificate authorities Kong Operator created for
// the GWAdmin: the API CA, the cp server CA and the published cp client CA.
func cCASecrets(t *testing.T, r *Reconciler) []*corev1.Secret {
	t.Helper()
	return []*corev1.Secret{
		getPrivateSecret(t, r, "gwadmin-gw-uid-api-ca"),
		getPrivateSecret(t, r, "gwadmin-gw-uid-cp-server-ca"),
		getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca"),
	}
}

// cNextExists reports whether the next cp client CA Secret exists.
func cNextExists(t *testing.T, r *Reconciler) bool {
	t.Helper()
	err := r.Get(t.Context(), types.NamespacedName{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca-next"}, &corev1.Secret{})
	if apierrors.IsNotFound(err) {
		return false
	}
	require.NoError(t, err)
	return true
}

// TestEnsureTLS_ClientRotationAnnotation checks that changing the rotation
// annotation of the GWAdmin rotates every certificate authority Kong
// Operator creates, recording the value on each, and that the same value
// rotates nothing again.
func TestEnsureTLS_ClientRotationAnnotation(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))

	before := cCASecrets(t, r)
	for _, s := range before {
		assert.Empty(t, s.Annotations[rotationTokenAnnotation], s.Name)
	}

	for _, token := range []string{"1", "2"} {
		gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: token}

		// The API and cp server CAs are renewed in place; the cp client CA
		// goes through the next Secret, and is published once trusted.
		m = ensure(t, r, gw, rolledDeployment(m))
		after := cCASecrets(t, r)
		for i, s := range after[:2] {
			assert.False(t, caCert(t, s).Equal(caCert(t, before[i])), "%s: rotated", s.Name)
			assert.Equal(t, token, s.Annotations[rotationTokenAnnotation], s.Name)
		}
		require.True(t, cNextExists(t, r))
		assert.Equal(t, token, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca-next").Annotations[rotationTokenAnnotation])
		assert.True(t, caCert(t, after[2]).Equal(caCert(t, before[2])), "not published before the pods trust it")

		m = ensure(t, r, gw, rolledDeployment(m))
		published := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")
		assert.False(t, caCert(t, published).Equal(caCert(t, before[2])), "published once trusted")
		assert.Equal(t, token, published.Annotations[rotationTokenAnnotation])
		assert.False(t, cNextExists(t, r))

		// The same value rotates nothing again.
		settled := cCASecrets(t, r)
		for range 3 {
			m = ensure(t, r, gw, rolledDeployment(m))
		}
		for i, s := range cCASecrets(t, r) {
			assert.True(t, caCert(t, s).Equal(caCert(t, settled[i])), "%s: rotated again", s.Name)
		}
		before = settled
	}
}

// TestEnsureTLS_ClientAutomaticCAValidity checks that the certificate
// authorities Kong Operator creates are valid for a year, and are renewed
// once two thirds of it have elapsed, not before.
func TestEnsureTLS_ClientAutomaticCAValidity(t *testing.T) {
	start := time.Now()
	now := start
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	m = ensure(t, r, gw, rolledDeployment(m))

	created := cCASecrets(t, r)
	for _, s := range created {
		ca := caCert(t, s)
		assert.WithinDuration(t, start.Add(consts.GWAdminCATTL), ca.NotAfter, time.Second, s.Name)
		// Backdated by 5 minutes against clock skew.
		assert.WithinDuration(t, start.Add(consts.GWAdminCATTL*2/3), renewalTime(ca), 5*time.Minute, s.Name)
	}

	renewal := renewalTime(caCert(t, created[0]))
	now = renewal.Add(-time.Minute)
	m = ensure(t, r, gw, rolledDeployment(m))
	for i, s := range cCASecrets(t, r) {
		assert.True(t, caCert(t, s).Equal(caCert(t, created[i])), "%s renewed early", s.Name)
	}
	assert.False(t, cNextExists(t, r))

	now = renewal.Add(time.Minute)
	ensure(t, r, gw, rolledDeployment(m))
	renewed := cCASecrets(t, r)
	for _, i := range []int{0, 1} {
		assert.False(t, caCert(t, renewed[i]).Equal(caCert(t, created[i])), "%s renewed", renewed[i].Name)
	}
	assert.True(t, cNextExists(t, r), "the cp client CA is renewed through the next Secret")
}

func TestDataPlaneContractValues(t *testing.T) {
	bundle := &corev1.Secret{Data: map[string][]byte{consts.CACRT: []byte("bundle")}}
	sum := sha256.Sum256([]byte("bundle"))
	assert.Equal(t, hex.EncodeToString(sum[:]), gwadminref.DataPlaneServerBundleChecksum(bundle))
	empty := sha256.Sum256(nil)
	assert.Equal(t, hex.EncodeToString(empty[:]), gwadminref.DataPlaneServerBundleChecksum(nil))

	ca := userCASecret(t, "ca")
	other := userCASecret(t, "other")
	der := sha256.Sum256(caCert(t, ca).Raw)
	assert.Equal(t, hex.EncodeToString(der[:]), gwadminref.DataPlaneClientCAFingerprint(ca.Data[corev1.TLSCertKey]))
	chain := append(append([]byte{}, ca.Data[corev1.TLSCertKey]...), other.Data[corev1.TLSCertKey]...)
	assert.Equal(t, hex.EncodeToString(der[:]), gwadminref.DataPlaneClientCAFingerprint(chain), "the first certificate")
	assert.Empty(t, gwadminref.DataPlaneClientCAFingerprint([]byte("nothing")))
}

func TestEnqueueForDataPlane(t *testing.T) {
	dp := dataPlaneDeploymentWith("dp-ns", "dp", "", "", true)
	assert.Equal(t, []reconcile.Request{{Namespace: testNamespace, Name: "gw"}},
		enqueueForDataPlane(t.Context(), dp))
	for _, label := range []string{consts.GWAdminDataPlaneNamespaceLabel, consts.GWAdminDataPlaneNameLabel} {
		partial := dp.DeepCopy()
		delete(partial.Labels, label)
		assert.Nil(t, enqueueForDataPlane(t.Context(), partial), label)
	}
	assert.Nil(t, enqueueForDataPlane(t.Context(), &appsv1.Deployment{}))
}

// TestEnsureTLS_ClientRemovingTheRotationAnnotationRotatesNothing checks that
// only setting the rotation annotation to a new value rotates the
// certificate authorities: removing it, e.g. a GitOps tool pruning it, does
// not.
func TestEnsureTLS_ClientRemovingTheRotationAnnotationRotatesNothing(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	api, server, client := apiSigner(t, r), cpServerSigner(t, r), publishedClientCA(t, r)

	gw.Annotations = nil
	for range 3 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}
	assert.True(t, apiSigner(t, r).Equal(api))
	assert.True(t, cpServerSigner(t, r).Equal(server))
	assert.True(t, publishedClientCA(t, r).Equal(client))

	// Setting it again, to the same value, rotates nothing either.
	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.True(t, apiSigner(t, r).Equal(api))

	// A new value rotates them.
	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "2"}
	ensure(t, r, gw, rolledDeployment(m))
	assert.False(t, apiSigner(t, r).Equal(api))
	assert.False(t, cpServerSigner(t, r).Equal(server))
}
