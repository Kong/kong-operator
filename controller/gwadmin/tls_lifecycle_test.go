package gwadmin

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const (
	bAPICASecretName       = "gwadmin-gw-uid-api-ca"
	bCPServerCASecretName  = "gwadmin-gw-uid-cp-server-ca"
	bOperatorClientTLSName = "gwadmin-gw-uid-operator-client-tls"
)

// bDataPlaneNamespace is the namespace of the data plane Deployments of the
// tests.
const bDataPlaneNamespace = "dp"

// TestEnsureTLS_StalledAPIRotationReissuesAtExpiry checks a CA rotation that
// stalls: the API CA changes and the Deployment never rolls out. The API
// certificates can't move to the new CA before the pods trust it, so they are
// reported blocked once due for renewal, the reconcile wakes up at their
// expiry, and they are then reissued from the new CA, as an expired
// certificate is broken for every peer anyway.
func TestEnsureTLS_StalledAPIRotationReissuesAtExpiry(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	rec := events.NewFakeRecorder(100)
	r.eventRecorder = rec

	m := ensure(t, r, gw, nil)
	certA := apiSigner(t, r)
	serverCA := cpServerSigner(t, r)
	requireSignedBy(t, certA, m.api, m.client)

	// The API CA is lost: a new one is created.
	require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bAPICASecretName)))
	m1 := ensure(t, r, gw, rolledDeployment(m))
	certB := apiSigner(t, r)
	require.False(t, certB.Equal(certA))
	require.Len(t, apiBundleCerts(m1), 2)
	assert.True(t, hasEvent(drainEvents(rec), "Normal CARotationStarted"))

	// The new template never rolls out.
	stalled := rollingDeployment(m1)
	const rolloutDescription = "waiting for the Deployment to roll out (0 of 1 pods updated, 0 available)"

	mid := ensure(t, r, gw, stalled)
	requireSignedBy(t, certA, mid.api, mid.client)
	cond := certificateCondition(gw)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason)
	assert.Contains(t, cond.Message, rolloutDescription)

	apiLeaf := leafCertificate(m1.api)
	expiry := apiLeaf.NotAfter

	// Past the renewal time: blocked.
	now = renewalTime(apiLeaf).Add(time.Minute)
	blocked, err := r.ensureTLS(t.Context(), gw, stalled)
	require.NoError(t, err, "blocked is reported in the condition, not as an error")
	requireSignedBy(t, certA, blocked.api, blocked.client)
	cond = certificateCondition(gw)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationBlocked, cond.Reason)
	assert.Contains(t, cond.Message, "gw-api-tls expires at "+expiry.UTC().Format(time.RFC3339))
	assert.Contains(t, cond.Message, bOperatorClientTLSName+" expires at")
	// The cp server certificate doesn't depend on the API CA: it is renewed
	// as usual.
	assert.NotContains(t, cond.Message, "gw-cp-tls")
	requireSignedBy(t, serverCA, blocked.cp)
	assert.True(t, now.Before(renewalTime(leafCertificate(blocked.cp))), "renewed")
	assert.Contains(t, cond.Message, rolloutDescription)
	assert.Positive(t, blocked.requeueAfter)
	assert.LessOrEqual(t, blocked.requeueAfter, expiry.Sub(now), "wakes up by the expiry at the latest")

	// Follow the requeues: the leaves stay on A, blocked, until they expire.
	m2 := blocked
	for passes := 0; now.Before(expiry); passes++ {
		require.Less(t, passes, 100, "too many passes")
		requireSignedBy(t, certA, m2.api)
		require.Equal(t, operatorv1alpha1.GWAdminReasonCARotationBlocked, certificateCondition(gw).Reason)
		require.GreaterOrEqual(t, m2.requeueAfter, time.Second)
		now = now.Add(m2.requeueAfter)
		require.False(t, now.After(expiry), "the requeue never overshoots the expiry")
		m2 = ensure(t, r, gw, stalled)
	}
	require.True(t, now.Equal(expiry), "the requeue lands on the expiry exactly")

	// Expired: reissued from B although the Deployment never rolled out.
	requireSignedBy(t, certB, m2.api, m2.client)
	for _, l := range leaves(m2) {
		assert.True(t, now.Before(leafCertificate(l).NotAfter), "%s is valid", l.Name)
	}
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, certificateCondition(gw).Reason)
	assert.True(t, hasEvent(drainEvents(rec), "Normal CertificateIssued Issued certificate gw-api-tls (expired)"))
	assert.LessOrEqual(t, m2.requeueAfter, maxRequeue)

	// Once the pods run them, A is dropped: the API domain doesn't wait for
	// data planes, Kong Operator is its only client.
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, apiBundleCerts(m3), 1)
	assert.True(t, apiBundleCerts(m3)[0].Equal(certB))
	assert.True(t, hasEvent(drainEvents(rec), "Normal CARotationCompleted"))
	// The trimmed bundle rolls the pods once more.
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, certificateCondition(gw).Reason)
	ensure(t, r, gw, rolledDeployment(m3))
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, certificateCondition(gw).Reason)
}

func TestEnsureTLS_ManualCPRenewal(t *testing.T) {
	t.Run("same issuer: copied right away, even while not rolled out", func(t *testing.T) {
		now := time.Now()
		public := userCASecret(t, "public")
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, userCPSecretFrom(t, public, now, 72*time.Hour, false))
		rec := events.NewFakeRecorder(100)
		r.eventRecorder = rec
		m := ensure(t, r, gw, nil)
		assert.True(t, containsCertificate(serverBundleCerts(m), caCert(t, public)), "its issuer is published")
		// A data plane runs the bundle trusting it.
		rollDataPlane(t, r, bDataPlaneNamespace, "dp", m)
		drainEvents(rec)

		renewed := userCPSecretFrom(t, public, now, 72*time.Hour, false)
		replaceUserCP(t, r, renewed)
		copied := ensure(t, r, gw, rollingDeployment(m))
		assert.Equal(t, renewed.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
		assert.Equal(t, renewed.Data[caBundleKey], copied.cp.Data[caBundleKey], "its issuers are kept along")
		assert.Equal(t, certificateSourceManual, copied.cp.Annotations[certificateSourceAnnotation])
		assert.Equal(t, DataPlaneServerBundleChecksum(m.serverBundle), DataPlaneServerBundleChecksum(copied.serverBundle),
			"the bundle didn't change")
		again := ensure(t, r, gw, rollingDeployment(copied))
		assert.Equal(t, renewed.Data[corev1.TLSCertKey], again.cp.Data[corev1.TLSCertKey])
		assert.True(t, hasEvent(drainEvents(rec), "Normal ManualCertificateCopied"))
	})

	t.Run("another issuer: waits for the data planes, blocked once due, copied once the copy expires", func(t *testing.T) {
		now := time.Now()
		public := userCASecret(t, "public")
		// The copy in use is short lived, the new certificate is not.
		first := userCPSecretFrom(t, public, now, 3*time.Hour, false)
		firstLeaf := leafCertificate(first)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, first)
		m := ensure(t, r, gw, nil)
		require.Equal(t, first.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey])
		rollDataPlane(t, r, bDataPlaneNamespace, "dp", m)

		public2 := userCASecret(t, "public-2")
		other := userCPSecretFrom(t, public2, now, 24*time.Hour, false)
		replaceUserCP(t, r, other)

		waiting := ensure(t, r, gw, rolledDeployment(m))
		assert.Equal(t, first.Data[corev1.TLSCertKey], waiting.cp.Data[corev1.TLSCertKey], "waits for the data planes")
		assert.True(t, containsCertificate(serverBundleCerts(waiting), caCert(t, public)))
		assert.True(t, containsCertificate(serverBundleCerts(waiting), caCert(t, public2)), "the new issuer is published first")
		cond := certificateCondition(gw)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason)
		assert.Contains(t, cond.Message, "the cp server certificate moves to its new issuer once every data plane runs "+
			"gw-cp-server-ca-bundle: data plane Deployment dp/dp doesn't run it yet")
		assert.Positive(t, waiting.requeueAfter)
		assert.LessOrEqual(t, waiting.requeueAfter, firstLeaf.NotAfter.Sub(now), "wakes up by the copy's expiry")

		// The data plane doesn't move: past the renewal time of the copy, the
		// rotation is blocked.
		now = renewalTime(firstLeaf).Add(time.Minute)
		blocked := ensure(t, r, gw, rolledDeployment(waiting))
		assert.Equal(t, first.Data[corev1.TLSCertKey], blocked.cp.Data[corev1.TLSCertKey])
		cond = certificateCondition(gw)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationBlocked, cond.Reason)
		assert.Contains(t, cond.Message, "gw-cp-tls expires at "+firstLeaf.NotAfter.UTC().Format(time.RFC3339))
		assert.Contains(t, cond.Message, "data plane Deployment dp/dp doesn't run it yet")

		// The copy expired: copied without waiting any longer.
		now = firstLeaf.NotAfter
		copied := ensure(t, r, gw, rolledDeployment(blocked))
		assert.Equal(t, other.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
		assert.Equal(t, metav1.ConditionTrue, certificateCondition(gw).Status)

		// Once every pod serves it, the previous issuer is no longer trusted.
		trimmed := ensure(t, r, gw, rolledDeployment(copied))
		assert.False(t, containsCertificate(serverBundleCerts(trimmed), caCert(t, public)))
		assert.True(t, containsCertificate(serverBundleCerts(trimmed), caCert(t, public2)))
	})

	t.Run("another issuer: copied once the data planes run the new bundle, without waiting for a gw-admin rollout", func(t *testing.T) {
		now := time.Now()
		first := userCPSecretFrom(t, userCASecret(t, "public"), now, 72*time.Hour, false)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, first)
		m := ensure(t, r, gw, nil)
		rollDataPlane(t, r, bDataPlaneNamespace, "dp-a", m)
		rollDataPlane(t, r, bDataPlaneNamespace, "dp-b", m)

		other := userCPSecretFrom(t, userCASecret(t, "public-2"), now, 72*time.Hour, false)
		replaceUserCP(t, r, other)
		stalled := rollingDeployment(m)
		waiting := ensure(t, r, gw, stalled)
		assert.Equal(t, first.Data[corev1.TLSCertKey], waiting.cp.Data[corev1.TLSCertKey])

		// One data plane moved, not the other.
		rollDataPlane(t, r, bDataPlaneNamespace, "dp-a", waiting)
		still := ensure(t, r, gw, stalled)
		assert.Equal(t, first.Data[corev1.TLSCertKey], still.cp.Data[corev1.TLSCertKey])
		assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp/dp-b doesn't run it yet")

		rollDataPlane(t, r, bDataPlaneNamespace, "dp-b", waiting)
		copied := ensure(t, r, gw, stalled)
		assert.Equal(t, other.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
	})

	t.Run("another issuer: a data plane rollout that doesn't complete keeps it waiting", func(t *testing.T) {
		now := time.Now()
		first := userCPSecretFrom(t, userCASecret(t, "public"), now, 72*time.Hour, false)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, first)
		m := ensure(t, r, gw, nil)
		rollDataPlane(t, r, bDataPlaneNamespace, "dp", m)

		replaceUserCP(t, r, userCPSecretFrom(t, userCASecret(t, "public-2"), now, 72*time.Hour, false))
		waiting := ensure(t, r, gw, rolledDeployment(m))
		// The controller managing the data plane applies the new bundle; its
		// pods never roll.
		setDataPlane(t, r, bDataPlaneNamespace, "dp", map[string]string{
			consts.GWAdminDataPlaneServerBundleAnnotation: DataPlaneServerBundleChecksum(waiting.serverBundle),
		}, false)
		again := ensure(t, r, gw, rolledDeployment(waiting))
		assert.Equal(t, first.Data[corev1.TLSCertKey], again.cp.Data[corev1.TLSCertKey])
		assert.Contains(t, certificateCondition(gw).Message,
			"data plane Deployment dp/dp: waiting for the Deployment to roll out (0 of 1 pods updated, 1 available)")
	})

	t.Run("another issuer, no data planes: copied right away", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, userCPSecretFrom(t, userCASecret(t, "public"), now, 72*time.Hour, false))
		m := ensure(t, r, gw, nil)
		other := userCPSecretFrom(t, userCASecret(t, "public-2"), now, 72*time.Hour, true)
		replaceUserCP(t, r, other)
		copied := ensure(t, r, gw, rolledDeployment(m))
		assert.Equal(t, other.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
	})

	t.Run("data planes of another GWAdmin don't count", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		foreign := dataPlaneDeploymentWith(bDataPlaneNamespace, "foreign", "stale", "stale", true)
		foreign.Labels[consts.GWAdminDataPlaneNameLabel] = "other"
		r := newReconciler(t, &now, gw, foreign, userCPSecretFrom(t, userCASecret(t, "public"), now, 72*time.Hour, false))
		grantDataPlanes(t, r, bDataPlaneNamespace)
		m := ensure(t, r, gw, nil)
		other := userCPSecretFrom(t, userCASecret(t, "public-2"), now, 72*time.Hour, false)
		replaceUserCP(t, r, other)
		copied := ensure(t, r, gw, rolledDeployment(m))
		assert.Equal(t, other.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
	})

	t.Run("the source expiry is due", func(t *testing.T) {
		now := time.Now()
		src := userCPSecretFrom(t, userCASecret(t, "public"), now, time.Hour, false)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, src)
		m := ensure(t, r, gw, nil)
		assert.LessOrEqual(t, m.requeueAfter, warningTime(leafCertificate(src)).Sub(now))
	})
}

// TestEnsureTLS_NewCAWithoutDeployment checks that, with no pods to wait
// for, a new cp client CA is published at once; the previous one stays
// trusted until every data plane runs a certificate from the new one, and is
// dropped at once without data planes; with pods, publishing it waits for
// them to trust it.
func TestEnsureTLS_NewCAWithoutDeployment(t *testing.T) {
	now := time.Now()
	caA := userCASecret(t, "ca-a")
	certA := caCert(t, caA)
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, caA)
	m := ensure(t, r, gw, nil)
	assert.True(t, publishedClientCA(t, r).Equal(certA))
	require.Len(t, clientBundleCerts(m), 1)
	// A data plane runs a certificate issued from A.
	rollDataPlane(t, r, bDataPlaneNamespace, "dp", m)

	certB := rotateManualCA(t, r, "ca-b")
	m1 := ensure(t, r, gw, nil)
	assert.True(t, publishedClientCA(t, r).Equal(certB), "published, no pod to wait for")
	require.Len(t, clientBundleCerts(m1), 2)
	assert.True(t, containsCertificate(clientBundleCerts(m1), certA), "the previous one, for the data plane")
	assert.True(t, m1.rotating)
	cond := certificateCondition(gw)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason)
	assert.Contains(t, cond.Message, "the previous cp client CA stays trusted until every data plane runs a "+
		"certificate issued from the new one: data plane Deployment dp/dp doesn't run it yet")

	// Stable without a Deployment, while the data plane doesn't move.
	m2 := ensure(t, r, gw, nil)
	assert.Equal(t, m1.checksum(), m2.checksum())
	require.Len(t, clientBundleCerts(ensure(t, r, gw, rolledDeployment(m2))), 2, "the data plane still runs A")

	// Once the data plane runs a certificate from B, A is dropped, and
	// forgotten by the published Secret.
	rollDataPlane(t, r, bDataPlaneNamespace, "dp", m2)
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, clientBundleCerts(m3), 1)
	assert.True(t, clientBundleCerts(m3)[0].Equal(certB))
	assert.NotContains(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data, previousCAKey)
	m3 = ensure(t, r, gw, rolledDeployment(m3))
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, certificateCondition(gw).Reason)

	// A data plane appearing later doesn't bring A back.
	grantDataPlanes(t, r, bDataPlaneNamespace)
	require.NoError(t, r.Create(t.Context(),
		dataPlaneDeploymentWith(bDataPlaneNamespace, "late", "", "", false)))
	late := ensure(t, r, gw, rolledDeployment(m3))
	require.Len(t, clientBundleCerts(late), 1)
	require.NoError(t, r.Delete(t.Context(), &appsv1.Deployment{Namespace: bDataPlaneNamespace, Name: "late"}))

	// Without data planes, the previous one is dropped as soon as the new
	// one is published; with a Deployment, publishing waits for the
	// rollout.
	require.NoError(t, r.Delete(t.Context(), &appsv1.Deployment{Namespace: bDataPlaneNamespace, Name: "dp"}))
	certC := rotateManualCA(t, r, "ca-c")
	m4 := ensure(t, r, gw, rolledDeployment(m3))
	assert.True(t, containsCertificate(clientBundleCerts(m4), certC), "trusted first")
	assert.True(t, publishedClientCA(t, r).Equal(certB))
	m5 := ensure(t, r, gw, rollingDeployment(m4))
	assert.True(t, publishedClientCA(t, r).Equal(certB), "not before the pods trust it")
	m6 := ensure(t, r, gw, rolledDeployment(m5))
	assert.True(t, publishedClientCA(t, r).Equal(certC))
	require.Len(t, clientBundleCerts(m6), 1)
	assert.True(t, clientBundleCerts(m6)[0].Equal(certC))
}

func TestEnsureTLS_ManualCPHostWarning(t *testing.T) {
	now := time.Now()
	public := userCASecret(t, "public")

	for _, tc := range []struct {
		name     string
		dnsNames []string
		warn     bool
	}{
		{name: "another host", dnsNames: []string{"cp.example.com"}, warn: true},
		{name: "the cp Service", dnsNames: []string{"gw-cp.ns.svc.cluster.local"}},
		{name: "a wildcard", dnsNames: []string{"*.ns.svc.cluster.local"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
			r := newReconciler(t, &now, gw, userCPSecretFrom(t, public, now, 24*time.Hour, false, tc.dnsNames...))
			ensure(t, r, gw, nil)
			cond := certificateCondition(gw)
			assert.Equal(t, metav1.ConditionTrue, cond.Status)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, cond.Reason)
			if tc.warn {
				assert.Contains(t, cond.Message, "doesn't cover gw-cp.ns.svc.cluster.local")
			} else {
				assert.Equal(t, "Certificates provisioned", cond.Message)
			}
		})
	}
}

// bSelfSignedServerCertificate returns the user's cp certificate Secret
// holding a self-signed server certificate, not a certificate authority.
func bSelfSignedServerCertificate(t *testing.T, now time.Time) *corev1.Secret {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(9),
		Subject:      pkix.Name{CommonName: "cp.example.com"},
		DNSNames:     []string{"cp.example.com"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	return &corev1.Secret{
		Name: userCPSecretName, Namespace: testNamespace, Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		},
	}
}

func TestEnsureTLS_ManualCPRejected(t *testing.T) {
	now := time.Now()
	issue := func(t *testing.T, withIssuer bool, usages ...x509.ExtKeyUsage) *corev1.Secret {
		t.Helper()
		ca := userCASecret(t, "public")
		certPEM, keyPEM, err := issueCertificate(ca, certRequest{
			commonName: "cp.example.com", dnsNames: []string{"cp.example.com"}, usages: usages, ttl: time.Hour,
		})
		require.NoError(t, err)
		s := &corev1.Secret{
			Name: userCPSecretName, Namespace: testNamespace, Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
		}
		if withIssuer {
			s.Data[caBundleKey] = ca.Data[corev1.TLSCertKey]
		}
		return s
	}

	for _, tc := range []struct {
		name    string
		tls     *operatorv1alpha1.GWAdminTLS
		objs    func(t *testing.T) []client.Object
		message string
	}{
		{
			name: "the Manual CA Secret",
			tls: &operatorv1alpha1.GWAdminTLS{
				ClientCA: manualCA(userCASecretName),
				CP: &operatorv1alpha1.GWAdminServerCertificate{
					Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
					SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: userCASecretName},
				},
			},
			objs:    func(t *testing.T) []client.Object { return []client.Object{userCASecret(t, "ca")} },
			message: "is the CA Secret",
		},
		{
			name: "a certificate authority",
			tls:  &operatorv1alpha1.GWAdminTLS{CP: manualCP()},
			objs: func(t *testing.T) []client.Object {
				ca := userCASecret(t, "ca")
				ca.Name = userCPSecretName
				return []client.Object{ca}
			},
			message: "holds a certificate authority",
		},
		{
			name: "a certificate with another certificate's key",
			tls:  &operatorv1alpha1.GWAdminTLS{CP: manualCP()},
			objs: func(t *testing.T) []client.Object {
				s := issue(t, true, x509.ExtKeyUsageServerAuth)
				s.Data[corev1.TLSPrivateKeyKey] = issue(t, true, x509.ExtKeyUsageServerAuth).Data[corev1.TLSPrivateKeyKey]
				return []client.Object{s}
			},
			message: "does not contain a valid tls.crt/tls.key pair",
		},
		{
			name: "a client certificate",
			tls:  &operatorv1alpha1.GWAdminTLS{CP: manualCP()},
			objs: func(t *testing.T) []client.Object {
				return []client.Object{issue(t, true, x509.ExtKeyUsageClientAuth)}
			},
			message: "can't be used by a server",
		},
		{
			name: "no issuer to verify it with",
			tls:  &operatorv1alpha1.GWAdminTLS{CP: manualCP()},
			objs: func(t *testing.T) []client.Object {
				return []client.Object{issue(t, false, x509.ExtKeyUsageServerAuth)}
			},
			message: "carries no issuer the data planes can verify it with",
		},
		{
			name: "an unrelated certificate authority in ca.crt",
			tls:  &operatorv1alpha1.GWAdminTLS{CP: manualCP()},
			objs: func(t *testing.T) []client.Object {
				s := issue(t, false, x509.ExtKeyUsageServerAuth)
				s.Data[caBundleKey] = userCASecret(t, "unrelated").Data[corev1.TLSCertKey]
				return []client.Object{s}
			},
			message: "carries no issuer the data planes can verify it with",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newGWAdmin(tc.tls)
			r := newReconciler(t, &now, append(tc.objs(t), gw)...)
			m, err := r.ensureTLS(t.Context(), gw, nil)
			require.ErrorIs(t, err, errStop)
			assert.Equal(t, maxRequeue, stopRequeue(err))
			cond := certificateCondition(gw)
			assert.Equal(t, metav1.ConditionFalse, cond.Status)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cond.Reason)
			assert.Contains(t, cond.Message, tc.message)
			assert.Nil(t, m.cp, "nothing is copied")
			err = r.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: "gw-cp-tls"}, &corev1.Secret{})
			assert.True(t, apierrors.IsNotFound(err), "nothing is copied: %v", err)
		})
	}

	for _, tc := range []struct {
		name   string
		secret func(t *testing.T) *corev1.Secret
	}{
		{name: "no extended key usage", secret: func(t *testing.T) *corev1.Secret {
			return issue(t, true)
		}},
		{name: "any extended key usage", secret: func(t *testing.T) *corev1.Secret {
			return issue(t, true, x509.ExtKeyUsageAny)
		}},
		{name: "server and client", secret: func(t *testing.T) *corev1.Secret {
			return issue(t, true, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
		}},
		{name: "issuer in the chain", secret: func(t *testing.T) *corev1.Secret {
			return userCPSecretFrom(t, userCASecret(t, "public"), now, time.Hour, true)
		}},
		{name: "self-signed", secret: func(t *testing.T) *corev1.Secret {
			return bSelfSignedServerCertificate(t, now)
		}},
	} {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			src := tc.secret(t)
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
			r := newReconciler(t, &now, gw, src)
			m := ensure(t, r, gw, nil)
			assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey])
			require.NoError(t, verifyWith(m.cp, serverBundleCerts(m), x509.ExtKeyUsageServerAuth),
				"the data planes can verify it with the cp server bundle")
		})
	}

	t.Run("transient read error", func(t *testing.T) {
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == userCPSecretName {
						return errors.New("etcd is down")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
		r := reconcilerWith(cl, &now)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorContains(t, err, "etcd is down")
		require.NotErrorIs(t, err, errStop)
		cond := certificateCondition(gw)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretReadFailed, cond.Reason)
	})
}

func TestEnsureTLS_MissingLeavesMidRotation(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	serverCA := cpServerSigner(t, r)

	// The API CA is lost: a new one is created, and the API listener moves
	// to it.
	require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bAPICASecretName)))
	m1 := ensure(t, r, gw, rolledDeployment(m))
	apiB := apiSigner(t, r)
	require.Len(t, apiBundleCerts(m1), 2)
	require.Len(t, clientBundleCerts(m1), 1, "the cp listener is not affected")
	require.Len(t, serverBundleCerts(m1), 1, "the cp listener is not affected")
	stalled := rollingDeployment(m1)

	require.NoError(t, r.Delete(t.Context(), getSecret(t, r, "gw-api-tls")))
	require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bOperatorClientTLSName)))

	// Both are issued right away from the new CA, mid-rotation, with the
	// rollout stalled: none is worse than one the old pods don't trust, and
	// the new pods only turn Ready once Kong Operator provisions them with
	// the client certificate, so waiting for the rollout would never end.
	m2 := ensure(t, r, gw, stalled)
	requireSignedBy(t, apiB, m2.api, m2.client)
	requireSignedBy(t, serverCA, m2.cp)
	assert.Equal(t, m1.trustChecksum(), m2.trustChecksum(), "the bundles didn't change")

	// The new pods trust the new bundle: nothing is issued again.
	m4 := ensure(t, r, gw, rolledWith(m1.checksum(), m2.trustChecksum()))
	requireSameLeaves(t, m2, m4)

	t.Run("without a Deployment, a missing client certificate is issued", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		ensure(t, r, gw, nil)
		require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bAPICASecretName)))
		require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bOperatorClientTLSName)))
		m := ensure(t, r, gw, nil)
		require.NotNil(t, m.client)
		requireSignedBy(t, apiSigner(t, r), m.client)
	})

	t.Run("no rotation: a missing client certificate is issued at once, even with a paused Deployment", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		m := ensure(t, r, gw, nil)
		before := leafCertificate(m.client)
		require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bOperatorClientTLSName)))

		paused := rollingDeployment(m)
		paused.Spec.Paused = true
		again := ensure(t, r, gw, paused)
		require.NotNil(t, again.client)
		assert.False(t, leafCertificate(again.client).Equal(before), "reissued")
		requireSignedBy(t, apiSigner(t, r), again.client)
		assert.Equal(t, m.checksum(), again.checksum(), "the mounted material didn't change")
		assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, certificateCondition(gw).Reason)
	})

	t.Run("a missing leaf with a Deployment on the same CA is issued", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		m := ensure(t, r, gw, nil)
		require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, bOperatorClientTLSName)))
		again := ensure(t, r, gw, rolledDeployment(m))
		require.NotNil(t, again.client)
	})
}

// bHiddenSecretName is the Secret bHidingClient hides.
const bHiddenSecretName = "gw-api-tls"

func TestEnsureTLS_StaleCacheAfterCreate(t *testing.T) {
	const name = bHiddenSecretName
	setup := func(t *testing.T, now *time.Time) (client.WithWatch, *operatorv1alpha1.GWAdmin) {
		t.Helper()
		gw := newGWAdmin(nil)
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw).Build()
		_, err := reconcilerWith(base, now).ensureTLS(t.Context(), gw, nil)
		require.NoError(t, err)
		return base, gw
	}

	for _, tc := range []struct{ namespace, name string }{
		{testNamespace, name},
		{testOperatorNamespace, bAPICASecretName},
	} {
		t.Run("the GWAdmin's own Secret: the cache is stale: "+tc.name, func(t *testing.T) {
			now := time.Now()
			base, gw := setup(t, &now)
			before := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), types.NamespacedName{Namespace: tc.namespace, Name: tc.name}, before))

			r := reconcilerWith(hidingClient(base, tc.name), &now)
			gw.Status.Conditions = nil
			_, err := r.ensureTLS(t.Context(), gw, nil)
			require.ErrorIs(t, err, errCacheStale)
			require.NotErrorIs(t, err, errStop)
			assert.Nil(t, certificateCondition(gw), "nothing is reported")

			after := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(before), after))
			assert.Equal(t, before.Data, after.Data, "left alone")
			assert.Equal(t, before.Labels, after.Labels)
		})
	}

	t.Run("the GWAdmin's own Secret, without its labels but the UID one: labeled back", func(t *testing.T) {
		now := time.Now()
		base, gw := setup(t, &now)
		live := &corev1.Secret{}
		require.NoError(t, base.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: name}, live))
		live.Labels = map[string]string{"user": "label", consts.GWAdminUIDLabel: "gw-uid"}
		require.NoError(t, base.Update(t.Context(), live))

		r := reconcilerWith(hidingClient(base, name), &now)
		gw.Status.Conditions = nil
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errCacheStale)
		assert.Nil(t, certificateCondition(gw))

		require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(live), live))
		assert.Equal(t, "true", live.Labels["konghq.com/secret"])
		assert.Equal(t, consts.SecretProvisioningAutomaticLabelValue, live.Labels[consts.SecretProvisioningLabelKey])
		assert.Equal(t, "gw", live.Labels[consts.GatewayOperatorManagedByNameLabel])
		assert.Equal(t, "label", live.Labels["user"], "the user's labels are kept")
	})

	// The GWAdmin's own Secret that lost its labels, the UID one included, is
	// recognized by its owner reference, and labelled again.
	for _, labels := range []map[string]string{{"user": "label"}, nil} {
		t.Run(fmt.Sprintf("the GWAdmin's own Secret without the UID label, labels %v", labels), func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			own := &corev1.Secret{Name: name, Namespace: testNamespace, Data: map[string][]byte{"k": []byte("v")}, Labels: labels}
			k8sutils.SetOwnerForObject(own, gw)
			base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, own).Build()
			r := reconcilerWith(hidingClient(base, name), &now)
			_, err := r.ensureTLS(t.Context(), gw, nil)
			require.ErrorIs(t, err, errCacheStale)
			assert.Nil(t, certificateCondition(gw))

			live := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(own), live))
			assert.Equal(t, string(gw.UID), live.Labels[consts.GWAdminUIDLabel])
			assert.Equal(t, "true", live.Labels["konghq.com/secret"])
			assert.Equal(t, labels["user"], live.Labels["user"], "the user's labels are kept")
		})
	}

	for _, tc := range []struct {
		name      string
		foreign   func(gw *operatorv1alpha1.GWAdmin) *corev1.Secret
		hidden    string
		wantInMsg string
	}{
		{
			name: "someone else's Secret: a conflict",
			foreign: func(*operatorv1alpha1.GWAdmin) *corev1.Secret {
				return &corev1.Secret{Name: name, Namespace: testNamespace, Data: map[string][]byte{"k": []byte("v")}}
			},
			hidden:    name,
			wantInMsg: "delete it, or rename it",
		},
		{
			name: "a Secret of a deleted GWAdmin with the same name: a conflict",
			foreign: func(gw *operatorv1alpha1.GWAdmin) *corev1.Secret {
				return &corev1.Secret{
					Name: name, Namespace: testNamespace, Data: map[string][]byte{"k": []byte("v")},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: operatorv1alpha1.SchemeGroupVersion.String(), Kind: operatorv1alpha1.GWAdminKind,
						Name: gw.Name, UID: "previous-uid", Controller: new(true),
					}},
				}
			},
			hidden:    name,
			wantInMsg: "delete it, or rename it",
		},
		{
			name: "someone else's Secret in the operator namespace: a conflict",
			foreign: func(*operatorv1alpha1.GWAdmin) *corev1.Secret {
				return &corev1.Secret{
					Name: bAPICASecretName, Namespace: testOperatorNamespace, Data: map[string][]byte{"k": []byte("v")},
					Labels: map[string]string{consts.GWAdminUIDLabel: "someone-else"},
				}
			},
			hidden:    bAPICASecretName,
			wantInMsg: "delete it, or rename it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			foreign := tc.foreign(gw)
			base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, foreign).Build()
			r := reconcilerWith(hidingClient(base, tc.hidden), &now)
			_, err := r.ensureTLS(t.Context(), gw, nil)
			require.ErrorIs(t, err, errStop)
			assert.Equal(t, conflictRequeue, stopRequeue(err))
			cond := certificateCondition(gw)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, cond.Reason)
			assert.Contains(t, cond.Message, tc.wantInMsg)
			live := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(foreign), live))
			assert.Equal(t, foreign.Data, live.Data)
			assert.Equal(t, foreign.Labels, live.Labels)
		})
	}

	t.Run("gone by the time it is patched: the cache is stale", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw).Build()
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if obj.GetName() == name {
					return apierrors.NewAlreadyExists(corev1.Resource("secrets"), name)
				}
				return c.Create(ctx, obj, opts...)
			},
		})
		r := reconcilerWith(cl, &now)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errCacheStale)
		assert.Nil(t, certificateCondition(gw))
	})

	t.Run("the patch fails", func(t *testing.T) {
		now := time.Now()
		base, gw := setup(t, &now)
		cl := interceptor.NewClient(hidingClient(base, name), interceptor.Funcs{
			Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
				return errors.New("apiserver unavailable")
			},
		})
		r := reconcilerWith(cl, &now)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorContains(t, err, "apiserver unavailable")
		require.NotErrorIs(t, err, errStop)
		require.NotErrorIs(t, err, errCacheStale)
	})

	t.Run("owned, but labeling it back fails", func(t *testing.T) {
		now := time.Now()
		base, gw := setup(t, &now)
		cl := interceptor.NewClient(hidingClient(base, name), interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if patch.Type() == types.MergePatchType {
					return errors.New("apiserver unavailable")
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		})
		r := reconcilerWith(cl, &now)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorContains(t, err, "failed to label Secret ns/gw-api-tls: apiserver unavailable")
		require.NotErrorIs(t, err, errCacheStale)
	})
}

func TestEnsureTLS_UpdateFailures(t *testing.T) {
	// setup provisions a GWAdmin over base and moves the clock so that the
	// next ensureTLS pass renews every leaf.
	setup := func(t *testing.T) (*time.Time, *operatorv1alpha1.GWAdmin, client.WithWatch, *tlsMaterial) {
		t.Helper()
		now := time.Now()
		gw := newGWAdmin(nil)
		base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw).Build()
		m, err := reconcilerWith(base, &now).ensureTLS(t.Context(), gw, nil)
		require.NoError(t, err)
		now = now.Add(consts.GWAdminLeafTTL * 3 / 4)
		return &now, gw, base, m
	}

	t.Run("conflict: the cache is stale", func(t *testing.T) {
		now, gw, base, m := setup(t)
		cl := interceptor.NewClient(base, interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				return apierrors.NewConflict(corev1.Resource("secrets"), obj.GetName(), errors.New("modified"))
			},
		})
		r := reconcilerWith(cl, now)
		gw.Status.Conditions = nil
		_, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
		require.ErrorIs(t, err, errCacheStale)
		assert.Nil(t, certificateCondition(gw))
	})

	for _, tc := range []struct {
		namespace, name string
		private         bool
	}{
		{testNamespace, "gw-api-tls", false},
		{testOperatorNamespace, bOperatorClientTLSName, true},
	} {
		t.Run("immutable: recreated: "+tc.name, func(t *testing.T) {
			now, gw, base, m := setup(t)
			// Give the Secret a UID, which the fake client doesn't set.
			live := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), types.NamespacedName{Namespace: tc.namespace, Name: tc.name}, live))
			live.UID = "leaf-uid"
			live.Immutable = new(true)
			require.NoError(t, base.Update(t.Context(), live))

			var deleted []string
			var preconditionUID types.UID
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if s, ok := obj.(*corev1.Secret); ok && s.Immutable != nil && *s.Immutable {
						return apierrors.NewInvalid(corev1.SchemeGroupVersion.WithKind("Secret").GroupKind(), obj.GetName(),
							field.ErrorList{field.Forbidden(field.NewPath("data"), "field is immutable when `immutable` is set")})
					}
					return c.Update(ctx, obj, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleted = append(deleted, obj.GetName())
					do := &client.DeleteOptions{}
					do.ApplyOptions(opts)
					if do.Preconditions != nil && do.Preconditions.UID != nil {
						preconditionUID = *do.Preconditions.UID
					}
					return c.Delete(ctx, obj, opts...)
				},
			})
			r := reconcilerWith(cl, now)
			renewed, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
			require.NoError(t, err)
			assert.Equal(t, []string{tc.name}, deleted)
			assert.Equal(t, types.UID("leaf-uid"), preconditionUID, "deleted with a UID precondition")

			recreated := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(live), recreated))
			assert.Nil(t, recreated.Immutable, "the new Secret is mutable")
			assert.Equal(t, corev1.SecretTypeTLS, recreated.Type)
			assert.Equal(t, consts.SecretProvisioningAutomaticLabelValue, recreated.Labels[consts.SecretProvisioningLabelKey])
			if tc.private {
				assert.False(t, leafCertificate(renewed.client).Equal(leafCertificate(m.client)), "renewed")
				assert.Equal(t, renewed.client.Data, recreated.Data)
				assert.Empty(t, recreated.OwnerReferences, "no owner reference across namespaces")
				assert.Equal(t, "gw-uid", recreated.Labels[consts.GWAdminUIDLabel])
			} else {
				assert.False(t, leafCertificate(renewed.api).Equal(leafCertificate(m.api)), "renewed")
				assert.Equal(t, renewed.api.Data, recreated.Data)
				assert.True(t, metav1.IsControlledBy(recreated, gw))
			}
		})
	}

	for _, tc := range []struct {
		name           string
		delete, create error
		wantErr        error
		wantInMsg      string
	}{
		{
			name:    "deleted in the meantime by someone else",
			delete:  apierrors.NewConflict(corev1.Resource("secrets"), "gw-api-tls", errors.New("UID mismatch")),
			wantErr: errCacheStale,
		},
		{
			name:      "the delete fails",
			delete:    errors.New("apiserver unavailable"),
			wantInMsg: "failed to delete Secret ns/gw-api-tls: apiserver unavailable",
		},
		{
			name:    "created in the meantime by someone else",
			create:  apierrors.NewAlreadyExists(corev1.Resource("secrets"), "gw-api-tls"),
			wantErr: errCacheStale,
		},
		{
			name:      "the create fails",
			create:    errors.New("apiserver unavailable"),
			wantInMsg: "failed to create Secret ns/gw-api-tls: apiserver unavailable",
		},
	} {
		t.Run("recreate: "+tc.name, func(t *testing.T) {
			now, gw, base, m := setup(t)
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if obj.GetName() == "gw-api-tls" {
						return apierrors.NewInvalid(corev1.SchemeGroupVersion.WithKind("Secret").GroupKind(), obj.GetName(), nil)
					}
					return c.Update(ctx, obj, opts...)
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if tc.delete != nil {
						return tc.delete
					}
					return c.Delete(ctx, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if tc.create != nil && obj.GetName() == "gw-api-tls" {
						return tc.create
					}
					return c.Create(ctx, obj, opts...)
				},
			})
			r := reconcilerWith(cl, now)
			_, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.ErrorContains(t, err, tc.wantInMsg)
			}
		})
	}

	t.Run("an owned Secret without labels is labeled back on update", func(t *testing.T) {
		now, gw, base, m := setup(t)
		live := &corev1.Secret{}
		require.NoError(t, base.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: "gw-api-tls"}, live))
		live.Labels = nil
		require.NoError(t, base.Update(t.Context(), live))

		renewed := ensure(t, reconcilerWith(base, now), gw, rolledDeployment(m))
		assert.Equal(t, "gw-uid", renewed.api.Labels[consts.GWAdminUIDLabel])
		require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(live), live))
		assert.Equal(t, "true", live.Labels["konghq.com/secret"])
	})
}

func TestEnsureTLS_ForeignNextCASecretIsLeftAlone(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)
	// Created once the client CA is published, which needs no next one.
	foreign := &corev1.Secret{Namespace: testOperatorNamespace, Name: cpClientCANextSecretName(gw), Data: map[string][]byte{"k": []byte("v")}}
	require.NoError(t, r.Create(t.Context(), foreign))
	ensure(t, r, gw, rolledDeployment(m))

	live := &corev1.Secret{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(foreign), live), "not the GWAdmin's: never deleted")
	assert.Equal(t, foreign.Data, live.Data)
}

func TestEnsureTLS_DeleteNextCAPrecondition(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	userCA := userCASecretValidAt(t, "user", now, 2*consts.GWAdminCATTL)
	base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, userCA).Build()
	var preconditionUID *types.UID
	cl := interceptor.NewClient(base, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			do := &client.DeleteOptions{}
			do.ApplyOptions(opts)
			if do.Preconditions != nil {
				preconditionUID = do.Preconditions.UID
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	r := reconcilerWith(cl, &now)
	m := ensure(t, r, gw, nil)

	// The cp client CA is due for renewal: the next one waits in
	// the -next Secret.
	now = renewalTime(publishedClientCA(t, r))
	ensure(t, r, gw, rolledDeployment(m))
	next := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca-next")
	next.UID = "next-uid"
	require.NoError(t, base.Update(t.Context(), next))

	// The Manual CA replaces it before it is published.
	gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}
	ensure(t, r, gw, rolledDeployment(m))
	require.NotNil(t, preconditionUID)
	assert.Equal(t, types.UID("next-uid"), *preconditionUID)
	err := r.Get(t.Context(), types.NamespacedName{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca-next"}, &corev1.Secret{})
	assert.True(t, apierrors.IsNotFound(err), "%v", err)
}

func TestEnsureTLS_ManualCAValidation(t *testing.T) {
	now := time.Now()
	ecParams := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07}})

	for _, tc := range []struct {
		name    string
		secret  func(t *testing.T) *corev1.Secret
		message string
	}{
		{
			name: "Ed25519 key",
			secret: func(t *testing.T) *corev1.Secret {
				_, priv, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				return selfSignedCA(t, now, priv, nil)
			},
			message: "Ed25519 keys are not supported",
		},
		{
			name: "tls.crt starts with something else",
			secret: func(t *testing.T) *corev1.Secret {
				s := userCASecret(t, "ca")
				s.Data[corev1.TLSCertKey] = append(append([]byte{}, ecParams...), s.Data[corev1.TLSCertKey]...)
				return s
			},
			message: "tls.crt must start with the certificate of the certificate authority",
		},
		{
			name: "tls.crt starts with another certificate",
			secret: func(t *testing.T) *corev1.Secret {
				s := userCASecret(t, "ca")
				other := userCASecret(t, "other")
				s.Data[corev1.TLSCertKey] = append(append([]byte{}, other.Data[corev1.TLSCertKey]...), s.Data[corev1.TLSCertKey]...)
				return s
			},
			message: "tls.crt and tls.key must hold a certificate and its key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
			r := newReconciler(t, &now, gw, tc.secret(t))
			for range 2 {
				_, err := r.ensureTLS(t.Context(), gw, nil)
				require.ErrorIs(t, err, errStop, "a stop, not an error retried with backoff")
				assert.Equal(t, maxRequeue, stopRequeue(err))
				cond := certificateCondition(gw)
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cond.Reason)
				assert.Contains(t, cond.Message, tc.message)
			}
		})
	}
}

func TestValidateCA(t *testing.T) {
	now := time.Now()
	ca := userCASecret(t, "ca")

	got, err := validateCA(ca, now)
	require.NoError(t, err)
	assert.True(t, got.Equal(caCert(t, ca)))

	_, err = validateCA(nil, now)
	require.ErrorContains(t, err, "missing")

	chained := ca.DeepCopy()
	chained.Data[corev1.TLSCertKey] = append(chained.Data[corev1.TLSCertKey], userCASecret(t, "root").Data[corev1.TLSCertKey]...)
	_, err = validateCA(chained, now)
	require.NoError(t, err, "a chain after the CA is accepted")

	_, err = validateCA(selfSignedCA(t, now, rsaKey(t, 2048), nil), now)
	require.NoError(t, err, "RSA")
}

func TestLeafSpecs(t *testing.T) {
	s := serverLeafSpec("gw-cp", "ns", "cluster.local")
	assert.Equal(t, "gw-cp", s.commonName)
	assert.Equal(t, []string{"gw-cp", "gw-cp.ns", "gw-cp.ns.svc", "gw-cp.ns.svc.cluster.local"}, s.dnsNames)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, s.usages)
	assert.True(t, s.mounted)

	c := operatorClientLeafSpec()
	assert.Equal(t, consts.GWAdminOperatorClientCommonName, c.commonName)
	assert.Empty(t, c.dnsNames)
	assert.False(t, c.mounted)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, c.usages)

	assert.Equal(t, []string{"s", "s.n", "s.n.svc"}, serverLeafSpec("s", "n", "").dnsNames, "no cluster domain")
}

func TestEnsureTLS_IssuedLeaves(t *testing.T) {
	now := time.Now()

	t.Run("ECDSA", func(t *testing.T) {
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		m := ensure(t, r, gw, nil)

		api := leafCertificate(m.api)
		assert.Equal(t, "gw-api", api.Subject.CommonName)
		assert.Equal(t, x509.KeyUsageDigitalSignature, api.KeyUsage, "no key encipherment for an ECDSA key")
		assert.Equal(t, "gw-cp", leafCertificate(m.cp).Subject.CommonName)
		assert.ElementsMatch(t, []string{"gw-cp", "gw-cp.ns", "gw-cp.ns.svc", "gw-cp.ns.svc.cluster.local"}, leafCertificate(m.cp).DNSNames)

		client := leafCertificate(m.client)
		assert.Equal(t, consts.GWAdminOperatorClientCommonName, client.Subject.CommonName)
		assert.Empty(t, client.DNSNames, "the client certificate names no host")
		assert.Empty(t, client.IPAddresses)
		assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, client.ExtKeyUsage)

		for _, tc := range []struct {
			secret *corev1.Secret
			cn     string
		}{
			{getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca"), "gw-admin cp client CA ns/gw"},
			{getPrivateSecret(t, r, bCPServerCASecretName), "gw-admin cp server CA ns/gw"},
			{getPrivateSecret(t, r, bAPICASecretName), "gw-admin API CA ns/gw"},
		} {
			ca := caCert(t, tc.secret)
			assert.Equal(t, tc.cn, ca.Subject.CommonName)
			assert.True(t, ca.MaxPathLenZero, tc.cn)
			assert.Equal(t, 0, ca.MaxPathLen, tc.cn)
			assert.Equal(t, consts.GWAdminCATTL+5*time.Minute, ca.NotAfter.Sub(ca.NotBefore), "valid for a year: %s", tc.cn)
		}

		// Stable: the client certificate without names is not reissued.
		again := ensure(t, r, gw, rolledDeployment(m))
		requireSameLeaves(t, m, again)
	})

	t.Run("RSA Manual client CA: the certificates Kong Operator issues don't depend on it", func(t *testing.T) {
		src := selfSignedCA(t, now, rsaKey(t, 2048), nil)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, src)
		m := ensure(t, r, gw, nil)
		assert.True(t, publishedClientCA(t, r).Equal(caCert(t, src)))
		for _, l := range leaves(m) {
			assert.Equal(t, x509.ECDSA, leafCertificate(l).PublicKeyAlgorithm, l.Name)
		}
		again := ensure(t, r, gw, rolledDeployment(m))
		requireSameLeaves(t, m, again)
	})

	t.Run("a leaf with the previous common name is reissued under the same CA", func(t *testing.T) {
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		m := ensure(t, r, gw, nil)
		caSecret := getPrivateSecret(t, r, bAPICASecretName)
		certPEM, keyPEM, err := issueCertificate(caSecret, certRequest{
			commonName: "gw-api.ns.svc", dnsNames: leafCertificate(m.api).DNSNames,
			usages: serverUsages, ttl: consts.GWAdminLeafTTL, now: now,
		})
		require.NoError(t, err)
		live := getSecret(t, r, "gw-api-tls")
		live.Data = map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}
		require.NoError(t, r.Update(t.Context(), live))

		// Not gated: same signer, the pods don't even need to roll.
		m2 := ensure(t, r, gw, rollingDeployment(m))
		assert.Equal(t, "gw-api", leafCertificate(m2.api).Subject.CommonName)
	})

	t.Run("configured validities are used, and renewal follows them", func(t *testing.T) {
		at := now
		gw := newGWAdmin(nil)
		r := newReconciler(t, &at, gw)
		r.CATTL, r.CertTTL = 2*time.Hour, 30*time.Minute
		m := ensure(t, r, gw, nil)
		for _, l := range leaves(m) {
			c := leafCertificate(l)
			assert.Equal(t, 30*time.Minute, c.NotAfter.Sub(c.NotBefore), l.Name)
		}
		for _, name := range []string{"gwadmin-gw-uid-cp-client-ca", bCPServerCASecretName, bAPICASecretName} {
			ca := caCert(t, getPrivateSecret(t, r, name))
			assert.Equal(t, 2*time.Hour+5*time.Minute, ca.NotAfter.Sub(ca.NotBefore), name)
		}

		// Two thirds of 30 minutes later, the leaves are issued again from
		// the same certificate authority, valid for 30 minutes again.
		at = at.Add(20 * time.Minute)
		m2 := ensure(t, r, gw, rolledDeployment(m))
		for _, l := range leaves(m2) {
			c := leafCertificate(l)
			assert.True(t, c.NotBefore.After(leafCertificate(m.api).NotBefore), "%s renewed", l.Name)
			assert.Equal(t, 30*time.Minute, c.NotAfter.Sub(c.NotBefore), l.Name)
		}
	})
}

func TestEnsureTLS_LongNames(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	gw.Name = strings.Repeat("g", 59)
	gw.Namespace = strings.Repeat("n", 63)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)

	s := &corev1.Secret{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Namespace: testOperatorNamespace, Name: CPClientCASecretName(gw)}, s))
	ca := caCert(t, s)
	assert.Len(t, ca.Subject.CommonName, 64)
	assert.Equal(t, ("gw-admin cp client CA " + gw.Namespace + "/" + gw.Name)[:64], ca.Subject.CommonName)
	for _, l := range leaves(m) {
		cn := leafCertificate(l).Subject.CommonName
		assert.LessOrEqual(t, len(cn), 64, l.Name)
	}
	assert.Equal(t, gw.Name+"-api", leafCertificate(m.api).Subject.CommonName)

	assert.Equal(t, "gw-admin cp client CA ns/gw", clientCACommonName(newGWAdmin(nil)))
	assert.Equal(t, "gw-admin cp server CA ns/gw", cpServerCACommonName(newGWAdmin(nil)))
}

func TestLeafNeedsIssue(t *testing.T) {
	now := time.Now()
	caSecret := userCASecret(t, "a")
	signer := caCert(t, caSecret)
	other := caCert(t, userCASecret(t, "b"))
	spec := serverLeafSpec("gw-api", "ns", "")
	issue := func(t *testing.T, cn string, names []string) *corev1.Secret {
		t.Helper()
		certPEM, keyPEM, err := issueCertificate(caSecret, certRequest{
			commonName: cn, dnsNames: names, usages: spec.usages, ttl: consts.GWAdminLeafTTL, now: now,
		})
		require.NoError(t, err)
		return &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}}
	}
	good := issue(t, spec.commonName, spec.dnsNames)
	clientSpec := operatorClientLeafSpec()

	for _, tc := range []struct {
		name    string
		secret  *corev1.Secret
		spec    leafSpec
		signer  *x509.Certificate
		at      time.Time
		running bool
		want    leafIssue
	}{
		{name: "up to date", secret: good, spec: spec, signer: signer, at: now, running: true},
		// A missing certificate is always issued at once: none is worse than
		// one the pods may not trust yet.
		{name: "missing mounted, running", spec: spec, signer: signer, at: now, running: true, want: leafIssue{needed: true}},
		{name: "missing client, running", spec: clientSpec, signer: signer, at: now, running: true, want: leafIssue{needed: true}},
		{name: "missing client, not running", spec: clientSpec, signer: signer, at: now, want: leafIssue{needed: true}},
		{name: "another signer, running", secret: good, spec: spec, signer: other, at: now, running: true, want: leafIssue{needed: true, gated: true}},
		{name: "another signer, not running", secret: good, spec: spec, signer: other, at: now, want: leafIssue{needed: true}},
		{name: "another common name", secret: issue(t, "old", spec.dnsNames), spec: spec, signer: signer, at: now, running: true, want: leafIssue{needed: true}},
		{name: "other names", secret: issue(t, spec.commonName, []string{"gw-api"}), spec: spec, signer: signer, at: now, want: leafIssue{needed: true}},
		{name: "due for renewal", secret: good, spec: spec, signer: signer, at: now.Add(consts.GWAdminLeafTTL * 3 / 4), running: true, want: leafIssue{needed: true}},
		{
			name:   "key doesn't match",
			secret: &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: good.Data[corev1.TLSCertKey], corev1.TLSPrivateKeyKey: issue(t, "x", nil).Data[corev1.TLSPrivateKeyKey]}},
			spec:   spec, signer: signer, at: now, running: true, want: leafIssue{needed: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, leafNeedsIssue(tc.secret, tc.spec, tc.signer, tc.at, tc.running, consts.GWAdminLeafTTL))
		})
	}
}

// TestEnsureTLS_TrustDomainsAreSeparate checks that each of the three trust
// domains only vouches for its own certificates: a data plane client
// certificate, issued from the published cp client CA, is accepted by the cp
// listener only, and nothing issued from that CA is a server certificate the
// data planes trust; the client certificate of Kong Operator is accepted by
// the API listener only; the cp server certificate verifies against the cp
// server bundle only.
func TestEnsureTLS_TrustDomainsAreSeparate(t *testing.T) {
	for _, tc := range []struct {
		name string
		tls  *operatorv1alpha1.GWAdminTLS
	}{
		{name: "automatic"},
		{name: "manual client CA", tls: &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}},
		{name: "manual client CA and cp", tls: &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName), CP: manualCP()}},
		{name: "manual cp", tls: &operatorv1alpha1.GWAdminTLS{CP: manualCP()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(tc.tls)
			src, _ := userCPSecret(t, time.Time{}, false)
			r := newReconciler(t, &now, gw, userCASecret(t, "user"), src)
			m := ensure(t, r, gw, nil)
			setStatus(gw, r.ClusterDomain)
			require.Equal(t, "gw-api-ca-bundle", gw.Status.TLS.APICABundleSecretName)
			require.Equal(t, "gw-cp-server-ca-bundle", gw.Status.TLS.CPServerCABundleSecretName)

			// What anyone reading the published cp client CA can issue: a
			// data plane client certificate, and a server certificate for
			// the cp listener.
			published := getPrivateSecret(t, r, CPClientCASecretName(gw))
			issue := func(spec leafSpec) *corev1.Secret {
				certPEM, keyPEM, err := issueCertificate(published, certRequest{
					commonName: spec.commonName, dnsNames: spec.dnsNames, usages: spec.usages, ttl: time.Hour, now: now,
				})
				require.NoError(t, err)
				return &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}}
			}
			dataPlane := issue(operatorClientLeafSpec())
			impostor := issue(serverLeafSpec("gw-cp", testNamespace, r.ClusterDomain))

			apiTrust, clientTrust, serverTrust := apiBundleCerts(m), clientBundleCerts(m), serverBundleCerts(m)

			require.NoError(t, verifyWith(dataPlane, clientTrust, x509.ExtKeyUsageClientAuth), "the cp listener accepts data planes")
			require.Error(t, verifyWith(dataPlane, apiTrust, x509.ExtKeyUsageClientAuth), "the API listener rejects data planes")
			require.Error(t, verifyWith(impostor, serverTrust, x509.ExtKeyUsageServerAuth), "data planes never trust a cp listener issued from the cp client CA")
			require.NoError(t, verifyWith(m.client, apiTrust, x509.ExtKeyUsageClientAuth), "the API listener accepts Kong Operator")
			require.Error(t, verifyWith(m.client, clientTrust, x509.ExtKeyUsageClientAuth), "the cp listener rejects Kong Operator")
			require.NoError(t, verifyWith(m.api, apiTrust, x509.ExtKeyUsageServerAuth), "Kong Operator trusts the API listener")
			require.NoError(t, verifyWith(m.cp, serverTrust, x509.ExtKeyUsageServerAuth), "data planes trust the cp listener")
			require.Error(t, verifyWith(m.cp, clientTrust, x509.ExtKeyUsageServerAuth))
			require.Error(t, verifyWith(m.cp, apiTrust, x509.ExtKeyUsageServerAuth))

			for _, pair := range [][2][]*x509.Certificate{{apiTrust, clientTrust}, {apiTrust, serverTrust}, {clientTrust, serverTrust}} {
				for _, c := range pair[0] {
					assert.False(t, containsCertificate(pair[1], c), "no certificate authority is in two bundles")
				}
			}
		})
	}
}

// TestEnsureTLS_CPServerCertificateNeverChainsToTheClientCA checks that the
// cp server certificate is issued from the cp server CA and stays put while
// the cp client CA rotates, Automatic or Manual.
func TestEnsureTLS_CPServerCertificateNeverChainsToTheClientCA(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tls    *operatorv1alpha1.GWAdminTLS
		rotate func(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin)
	}{
		{
			name: "automatic client CA",
			rotate: func(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin) {
				// Rotated: only the cp client CA, the others already run under
				// the requested value.
				gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
				for _, name := range []string{"gwadmin-gw-uid-api-ca", "gwadmin-gw-uid-cp-server-ca"} {
					live := getPrivateSecret(t, r, name)
					live.Annotations[rotationTokenAnnotation] = "1"
					require.NoError(t, r.Update(t.Context(), live))
				}
			},
		},
		{
			name:   "manual client CA",
			tls:    &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)},
			rotate: func(t *testing.T, r *Reconciler, _ *operatorv1alpha1.GWAdmin) { rotateManualCA(t, r, "ca-b") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(tc.tls)
			r := newReconciler(t, &now, gw, userCASecret(t, "ca-a"))
			m := ensure(t, r, gw, nil)
			serverCA := cpServerSigner(t, r)
			requireSignedBy(t, serverCA, m.cp)
			first := leafCertificate(m.cp)

			clientCAs := []*x509.Certificate{publishedClientCA(t, r)}
			tc.rotate(t, r, gw)

			// Through every phase of the cp client CA rotation.
			d := rolledDeployment(m)
			for range 4 {
				m = ensure(t, r, gw, d)
				clientCAs = union(clientCAs, clientBundleCerts(m), []*x509.Certificate{publishedClientCA(t, r)})
				d = rolledDeployment(m)
			}
			m = ensure(t, r, gw, d)
			require.Len(t, clientCAs, 2, "the cp client CA rotated")
			require.Len(t, clientBundleCerts(m), 1, "and the rotation completed")

			assert.True(t, leafCertificate(m.cp).Equal(first), "the cp server certificate never moved")
			for _, ca := range clientCAs {
				requireNotSignedBy(t, ca, m.cp)
				assert.False(t, containsCertificate(serverBundleCerts(m), ca))
			}
		})
	}
}

// TestCleanup_DeletesPrivateSecretsTheCacheDoesntSee checks that the Secrets
// of a deleted GWAdmin in the operator namespace are deleted even when the
// cache doesn't see them, e.g. after they lost their labels: their key is
// never left behind.
func TestCleanup_DeletesPrivateSecretsTheCacheDoesntSee(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	gw.Finalizers = []string{consts.GWAdminCleanupFinalizer}
	hidden := &corev1.Secret{Name: "gwadmin-gw-uid-api-ca", Namespace: testOperatorNamespace, Data: map[string][]byte{"tls.key": []byte("k")}}
	other := &corev1.Secret{Name: "gwadmin-other-uid-api-ca", Namespace: testOperatorNamespace}
	base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, hidden, other).Build()
	r := reconcilerWith(hidingClient(base, hidden.Name), &now)

	require.NoError(t, r.cleanup(t.Context(), gw))
	assert.True(t, apierrors.IsNotFound(base.Get(t.Context(), client.ObjectKeyFromObject(hidden), &corev1.Secret{})))
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(other), &corev1.Secret{}), "another GWAdmin's Secret is kept")
	live := &operatorv1alpha1.GWAdmin{}
	require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(gw), live))
	assert.NotContains(t, live.Finalizers, consts.GWAdminCleanupFinalizer)
}
