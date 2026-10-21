package gwadmin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// dRotate changes the rotation annotation of gw, rotating every Automatic CA.
func dRotate(gw *operatorv1alpha1.GWAdmin) {
	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
}

// dSteady provisions gw from scratch and returns the material once the pods
// run it.
func dSteady(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin) *tlsMaterial {
	t.Helper()
	m := ensure(t, r, gw, nil)
	return ensure(t, r, gw, rolledDeployment(m))
}

const (
	dDataPlaneNamespace = "dp-ns"
	dDataPlaneName      = "dp"
)

func TestEnsureTLS_ServerCARotationWaitsForTheDataPlanes(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := dSteady(t, r, gw)
	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
	old := cpServerSigner(t, r)
	requireSignedBy(t, old, m.cp)
	require.Len(t, serverBundleCerts(m), 1)

	dRotate(gw)
	m = ensure(t, r, gw, rolledDeployment(m))
	next := cpServerSigner(t, r)
	require.False(t, next.Equal(old), "the cp server CA was renewed")

	// The new CA is trusted at once, the cp certificate stays on the old one:
	// the data plane doesn't run that bundle yet.
	certs := serverBundleCerts(m)
	assert.True(t, containsCertificate(certs, old))
	assert.True(t, containsCertificate(certs, next))
	requireSignedBy(t, old, m.cp)
	cond := certificateCondition(gw)
	require.NotNil(t, cond)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason)
	assert.Contains(t, cond.Message, "data plane Deployment dp-ns/dp doesn't run it yet")

	// The data plane records the new bundle, but its rollout isn't complete.
	setDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, map[string]string{consts.GWAdminDataPlaneServerBundleAnnotation: DataPlaneServerBundleChecksum(m.serverBundle)}, false)
	m = ensure(t, r, gw, rolledDeployment(m))
	requireSignedBy(t, old, m.cp)
	assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp-ns/dp: waiting for the Deployment to roll out")

	// Once it rolled out, the certificate moves, without waiting for the
	// gw-admin rollout.
	setDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, map[string]string{consts.GWAdminDataPlaneServerBundleAnnotation: DataPlaneServerBundleChecksum(m.serverBundle)}, true)
	stuck := rollingDeployment(m)
	m = ensure(t, r, gw, stuck)
	requireSignedBy(t, next, m.cp)
	assert.True(t, containsCertificate(serverBundleCerts(m), old), "old pods still serve the old certificate")

	// The old CA is dropped once the pods serve the new certificate.
	m = ensure(t, r, gw, stuck)
	assert.True(t, containsCertificate(serverBundleCerts(m), old))
	// The rotated API certificates move too once the pods roll: roll until
	// the pods run everything.
	for range 3 {
		m = ensure(t, r, gw, rolledDeployment(m))
	}
	require.Len(t, serverBundleCerts(m), 1)
	assert.True(t, serverBundleCerts(m)[0].Equal(next))
}

func TestEnsureTLS_ServerCARotationWithoutDataPlanes(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	// Deployments that are no data plane of gw don't count.
	unlabeled := dataPlaneDeploymentWith(dDataPlaneNamespace, "unlabeled", "", "", false)
	unlabeled.Labels = nil
	another := dataPlaneDeploymentWith(dDataPlaneNamespace, "another", "", "", false)
	another.Labels[consts.GWAdminDataPlaneNameLabel] = "other"
	elsewhere := dataPlaneDeploymentWith(dDataPlaneNamespace, "elsewhere", "", "", false)
	elsewhere.Labels[consts.GWAdminDataPlaneNamespaceLabel] = "other-ns"
	r := newReconciler(t, &now, gw, unlabeled, another, elsewhere)
	grantDataPlanes(t, r, dDataPlaneNamespace)
	m := dSteady(t, r, gw)
	old := cpServerSigner(t, r)

	dRotate(gw)
	m = ensure(t, r, gw, rolledDeployment(m))
	next := cpServerSigner(t, r)
	require.False(t, next.Equal(old))
	requireSignedBy(t, next, m.cp)
}

func TestEnsureTLS_ServerCARotationBlockedByADataPlane(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := dSteady(t, r, gw)
	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
	old := cpServerSigner(t, r)

	dRotate(gw)
	m = ensure(t, r, gw, rolledDeployment(m))
	next := cpServerSigner(t, r)
	requireSignedBy(t, old, m.cp)
	leaf := leafCertificate(m.cp)

	// Past its renewal time it still waits, reported blocked: it is not
	// broken yet. Its expiry is looked at.
	now = renewalTime(leaf).Add(time.Hour)
	m = ensure(t, r, gw, rolledDeployment(m))
	requireSignedBy(t, old, m.cp)
	assert.LessOrEqual(t, m.requeueAfter, leaf.NotAfter.Sub(now))
	cond := certificateCondition(gw)
	require.NotNil(t, cond)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationBlocked, cond.Reason)
	assert.Contains(t, cond.Message, "gw-cp-tls expires at "+leaf.NotAfter.UTC().Format(time.RFC3339))
	assert.Contains(t, cond.Message, "dp-ns/dp")

	// Expired, it is broken for everyone: it moves at once.
	now = leaf.NotAfter
	m = ensure(t, r, gw, rollingDeployment(m))
	requireSignedBy(t, next, m.cp)
}

func TestEnsureTLS_ServerBundleIsNotMounted(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := dSteady(t, r, gw)
	sum, trust := m.checksum(), m.trustChecksum()

	m.serverBundle = m.serverBundle.DeepCopy()
	m.serverBundle.Data[caBundleKey] = append(m.serverBundle.Data[caBundleKey], m.serverBundle.Data[caBundleKey]...)
	assert.Equal(t, sum, m.checksum())
	assert.Equal(t, trust, m.trustChecksum())
	m.serverBundle = nil
	assert.Equal(t, sum, m.checksum())
	assert.True(t, m.mountable())
}

// dIntermediate returns a CA Secret named name, holding an intermediate
// certificate authority signed by the one of root.
func dIntermediate(t *testing.T, root *corev1.Secret, name string, now time.Time) *corev1.Secret {
	t.Helper()
	rootCert := caCert(t, root)
	block, _ := pem.Decode(root.Data[corev1.TLSPrivateKeyKey])
	rootKey, err := x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(300 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, rootCert, &key.PublicKey, rootKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return &corev1.Secret{
		Name: name, Namespace: testNamespace,
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		},
	}
}

func TestEnsureTLS_ServerManualCertificate(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	public := userCASecret(t, "public")
	src := userCPSecretFrom(t, public, now, 30*24*time.Hour, false)
	r := newReconciler(t, &now, gw, src)
	m := dSteady(t, r, gw)
	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
	server := cpServerSigner(t, r)

	// Automatic to Manual: the user's issuer is trusted at once, the
	// certificate is copied once the data plane runs that bundle.
	gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{CP: manualCP()}
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.True(t, containsCertificate(serverBundleCerts(m), caCert(t, public)), "issuer from ca.crt")
	assert.True(t, containsCertificate(serverBundleCerts(m), server), "the cp server CA stays trusted")
	requireSignedBy(t, server, m.cp)
	assert.False(t, isManualCopy(m.cp))
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, certificateCondition(gw).Reason)
	assert.Contains(t, certificateCondition(gw).Message, "dp-ns/dp")

	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
	m = ensure(t, r, gw, rolledDeployment(m))
	require.True(t, isManualCopy(m.cp))
	assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey])
	assert.Equal(t, src.Data[caBundleKey], m.cp.Data[caBundleKey], "the copy keeps ca.crt")

	// A renewal from the same issuer, the data plane running the current
	// bundle, is copied at once.
	renewed := userCPSecretFrom(t, public, now, 30*24*time.Hour, false)
	replaceUserCP(t, r, renewed)
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.Equal(t, renewed.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey])

	// The pods serve it: the server bundle holds the cp server CA and the
	// user's issuer only.
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.Len(t, serverBundleCerts(m), 2)

	// Manual to Automatic is immediate: the cp server CA is always in the
	// bundle the data plane runs.
	gw.Spec.TLS = nil
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.False(t, isManualCopy(m.cp))
	requireSignedBy(t, server, m.cp)
}

// dRoot returns a root certificate authority Secret that, unlike the ones of
// generateCA, may sign intermediate ones.
func dRoot(t *testing.T, now time.Time) *corev1.Secret {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "root"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(2 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true, MaxPathLen: 1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return &corev1.Secret{Data: map[string][]byte{
		corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}}
}

func TestEnsureTLS_ServerManualIntermediateChangesUnderTheSameRoot(t *testing.T) {
	now := time.Now()
	root := dRoot(t, now)
	issue := func(intermediate *corev1.Secret) *corev1.Secret {
		s := userCPSecretFrom(t, intermediate, now, 30*24*time.Hour, true)
		s.Data[caBundleKey] = root.Data[corev1.TLSCertKey]
		return s
	}
	intA := dIntermediate(t, root, "int-a", now)
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
	r := newReconciler(t, &now, gw, issue(intA))
	m := dSteady(t, r, gw)
	assert.True(t, containsCertificate(serverBundleCerts(m), caCert(t, root)))
	assert.True(t, containsCertificate(serverBundleCerts(m), caCert(t, intA)))
	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)

	// A certificate from another intermediate under the same root: the root
	// is in the bundle the data plane runs, so the data plane could verify
	// it right away.
	intB := dIntermediate(t, root, "int-b", now)
	next := issue(intB)
	replaceUserCP(t, r, next)
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.True(t, containsCertificate(serverBundleCerts(m), caCert(t, intB)), "the new intermediate is trusted too")
	assert.Equal(t, next.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey], "copied right away")
	assert.True(t, containsCertificate(anchorsOf(m.cp, now), caCert(t, root)))
}

// dExpiringEvents counts the CertificateExpiringSoon warnings among evts.
func dExpiringEvents(evts []string) int {
	n := 0
	for _, e := range evts {
		if strings.HasPrefix(e, "Warning CertificateExpiringSoon") {
			n++
		}
	}
	return n
}

func TestReconcile_ServerManualCertificateExpiringSoon(t *testing.T) {
	issued := time.Now()
	src := userCPSecretFrom(t, userCASecret(t, "public"), issued, 60*24*time.Hour, false, "gw-cp.ns.svc.cluster.local")
	leaf := leafCertificate(src)
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()}), src)

	// Requeued at the warning onset.
	e.now = warningTime(leaf).Add(-time.Hour)
	res, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Positive(t, res.RequeueAfter)
	assert.LessOrEqual(t, res.RequeueAfter, time.Hour)
	assert.NotContains(t, certificateCondition(gw).Message, "expires at")
	assert.Zero(t, dExpiringEvents(drainEvents(e.rec)))

	e.now = warningTime(leaf).Add(time.Minute)
	_, gw, err = e.reconcile(t)
	require.NoError(t, err)
	assert.Contains(t, certificateCondition(gw).Message,
		`cp certificate Secret "user-cp" expires at `+leaf.NotAfter.UTC().Format(time.RFC3339))
	evts := drainEvents(e.rec)
	assert.Equal(t, 1, dExpiringEvents(evts), "%v", evts)

	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	assert.Zero(t, dExpiringEvents(drainEvents(e.rec)), "warned once")
}

func TestReconcile_ServerManualClientCAExpiringSoon(t *testing.T) {
	ca := userCASecretValidAt(t, "user", time.Now(), 60*24*time.Hour)
	cert := caCert(t, ca)
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}), ca)

	e.now = warningTime(cert).Add(-time.Hour)
	res, _, err := e.reconcile(t)
	require.NoError(t, err)
	assert.LessOrEqual(t, res.RequeueAfter, time.Hour)
	assert.Zero(t, dExpiringEvents(drainEvents(e.rec)))

	e.now = warningTime(cert).Add(time.Minute)
	for i := range 2 {
		_, gw, err := e.reconcile(t)
		require.NoError(t, err)
		assert.Contains(t, certificateCondition(gw).Message,
			`CA Secret "user-ca" expires at `+cert.NotAfter.UTC().Format(time.RFC3339))
		assert.Equal(t, 1-i, dExpiringEvents(drainEvents(e.rec)), "pass %d", i)
	}
}

// TestReconcile_ServerHostWarningIsNoExpiry checks that a warning which is
// not about an expiry isn't reported as one.
func TestReconcile_ServerHostWarningIsNoExpiry(t *testing.T) {
	src := userCPSecretFrom(t, userCASecret(t, "public"), time.Now(), 60*24*time.Hour, false, "cp.example.com")
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()}), src)

	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	assert.Contains(t, certificateCondition(gw).Message, "doesn't cover")
	evts := drainEvents(e.rec)
	assert.Zero(t, dExpiringEvents(evts), "%v", evts)
}

func TestServerEnqueueForPrivateSecret(t *testing.T) {
	s := &corev1.Secret{Labels: map[string]string{
		consts.GWAdminUIDLabel:                        "gw-uid",
		consts.GatewayOperatorManagedByNamespaceLabel: testNamespace,
		consts.GatewayOperatorManagedByNameLabel:      "gw",
	}}
	reqs := enqueueForPrivateSecret(t.Context(), s)
	require.Len(t, reqs, 1)
	assert.Equal(t, types.NamespacedName{Namespace: testNamespace, Name: "gw"}, reqs[0].NamespacedName)
	assert.Nil(t, enqueueForPrivateSecret(t.Context(), &corev1.Secret{}))

	// The Secrets in the GWAdmin namespace carry the UID label too: their
	// owner reference maps them.
	public := s.DeepCopy()
	delete(public.Labels, consts.GatewayOperatorManagedByNamespaceLabel)
	assert.Nil(t, enqueueForPrivateSecret(t.Context(), public))
}

func TestReconcile_ServerFinalizerCleansUpPrivateSecrets(t *testing.T) {
	other := &corev1.Secret{
		Name: "gwadmin-other-uid-api-ca", Namespace: testOperatorNamespace,
		Labels: map[string]string{consts.GWAdminUIDLabel: "other-uid"},
	}
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil), other)
	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	require.True(t, controllerutil.ContainsFinalizer(gw, consts.GWAdminCleanupFinalizer))
	private := []string{"gwadmin-gw-uid-api-ca", "gwadmin-gw-uid-cp-server-ca", "gwadmin-gw-uid-operator-client-tls"}
	for _, name := range private {
		require.NoError(t, e.cl.Get(t.Context(), client.ObjectKey{Namespace: testOperatorNamespace, Name: name}, &corev1.Secret{}))
	}

	require.NoError(t, e.cl.Delete(t.Context(), gw))
	deleting := e.gw(t)
	require.False(t, deleting.DeletionTimestamp.IsZero())
	_, err = e.r.Reconcile(t.Context(), deleting)
	require.NoError(t, err)

	for _, name := range private {
		err := e.cl.Get(t.Context(), client.ObjectKey{Namespace: testOperatorNamespace, Name: name}, &corev1.Secret{})
		assert.True(t, apierrors.IsNotFound(err), "%s: %v", name, err)
	}
	require.NoError(t, e.cl.Get(t.Context(), client.ObjectKeyFromObject(other), &corev1.Secret{}), "another GWAdmin's Secret stays")
	err = e.cl.Get(t.Context(), client.ObjectKey{Namespace: testNamespace, Name: "gw"}, &operatorv1alpha1.GWAdmin{})
	assert.True(t, apierrors.IsNotFound(err), "the finalizer is gone: %v", err)
}
