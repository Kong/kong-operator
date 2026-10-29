package gwadmin

import (
	"crypto"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestEnsureTLS_AutomaticProvisioning(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)

	m, err := r.ensureTLS(t.Context(), gw, nil)
	require.NoError(t, err)

	// The cp client CA is published in the operator namespace: its key never
	// lands in the GWAdmin namespace.
	ca := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")
	assert.Equal(t, string(gw.UID), ca.Labels[consts.GWAdminUIDLabel])
	assert.Empty(t, ca.OwnerReferences)
	assert.Equal(t, "true", ca.Labels["konghq.com/secret"])
	clientCA := caCert(t, ca)
	require.True(t, clientCA.IsCA)
	require.Len(t, clientBundleCerts(m), 1)
	assert.True(t, clientBundleCerts(m)[0].Equal(clientCA))
	assert.False(t, m.rotating)

	// The cp server certificate comes from the cp server CA, kept in the
	// operator namespace: never from the published cp client CA.
	serverCASecret := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-server-ca")
	assert.Empty(t, serverCASecret.OwnerReferences, "no owner reference crosses namespaces")
	assert.Equal(t, "gw-uid", serverCASecret.Labels[consts.GWAdminUIDLabel])
	serverCA := caCert(t, serverCASecret)
	assert.Equal(t, "gw-admin cp server CA ns/gw", serverCA.Subject.CommonName)
	requireSignedBy(t, serverCA, m.cp)
	requireNotSignedBy(t, clientCA, m.cp)
	require.Len(t, serverBundleCerts(m), 1)
	assert.True(t, serverBundleCerts(m)[0].Equal(serverCA))
	assert.False(t, containsCertificate(clientBundleCerts(m), serverCA), "the cp listener never accepts it for clients")

	// The API listener has a certificate authority of its own, in the
	// operator namespace, like the operator client certificate.
	apiCA := getPrivateSecret(t, r, "gwadmin-gw-uid-api-ca")
	assert.Empty(t, apiCA.OwnerReferences)
	assert.Equal(t, "gw-uid", apiCA.Labels[consts.GWAdminUIDLabel])
	api := caCert(t, apiCA)
	require.True(t, api.IsCA)
	assert.Equal(t, "gw-admin API CA ns/gw", api.Subject.CommonName)
	assert.False(t, api.Equal(clientCA))
	assert.False(t, api.Equal(serverCA))
	require.Len(t, apiBundleCerts(m), 1)
	assert.True(t, apiBundleCerts(m)[0].Equal(api))
	requireSignedBy(t, api, m.api, m.client)
	requireNotSignedBy(t, clientCA, m.api, m.client)
	requireNotSignedBy(t, api, m.cp)
	assert.Equal(t, testOperatorNamespace, m.client.Namespace)
	assert.Equal(t, "gwadmin-gw-uid-operator-client-tls", m.client.Name)

	apiLeaf := leafCertificate(m.api)
	assert.ElementsMatch(t, []string{"gw-api", "gw-api.ns", "gw-api.ns.svc", "gw-api.ns.svc.cluster.local"}, apiLeaf.DNSNames)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, apiLeaf.ExtKeyUsage)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leafCertificate(m.client).ExtKeyUsage)
	assert.Contains(t, leafCertificate(m.cp).DNSNames, "gw-cp.ns.svc")

	// Nothing changes on the next pass.
	again, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.NoError(t, err)
	assert.Equal(t, m.checksum(), again.checksum())
	assert.Equal(t, m.serverBundle.Data, again.serverBundle.Data)
	assert.Equal(t, m.serverBundle.Annotations, again.serverBundle.Annotations)
	// The leaves are due in 60 days and the CAs in months: the daily
	// re-validation comes first.
	assert.Equal(t, pki.MaxRenewalRequeue, m.requeueAfter)
	assert.Equal(t, pki.MaxRenewalRequeue, again.requeueAfter)
}

func TestEnsureTLS_LeafRenewalIsNotGated(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)

	m, err := r.ensureTLS(t.Context(), gw, nil)
	require.NoError(t, err)
	before := leafCertificate(m.api)
	beforeCP := leafCertificate(m.cp)

	// Past two thirds of the leaf validity, with the pods not rolled.
	now = now.Add(consts.GWAdminLeafTTL * 3 / 4)
	renewed, err := r.ensureTLS(t.Context(), gw, nil)
	require.NoError(t, err)
	assert.False(t, leafCertificate(renewed.api).Equal(before), "the certificate is renewed")
	assert.False(t, leafCertificate(renewed.cp).Equal(beforeCP), "the cp certificate is renewed under the same issuer")
	assert.Len(t, clientBundleCerts(renewed), 1, "the CA did not change")
	assert.Len(t, serverBundleCerts(renewed), 1)
	assert.False(t, renewed.rotating)
}

// TestEnsureTLS_CARotationPhases follows a rotation of the Manual cp client
// CA: it only touches the bundle the cp listener verifies the data planes
// with, and the published CA. No certificate Kong Operator issues moves.
func TestEnsureTLS_CARotationPhases(t *testing.T) {
	now := time.Now()
	caA := userCASecret(t, "ca-a")
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, caA)
	certA := caCert(t, caA)

	m, err := r.ensureTLS(t.Context(), gw, nil)
	require.NoError(t, err)
	assert.True(t, publishedClientCA(t, r).Equal(certA), "published, nothing to wait for")
	require.Len(t, clientBundleCerts(m), 1)
	assert.True(t, clientBundleCerts(m)[0].Equal(certA))
	// The user's certificate authority signs nothing Kong Operator issues.
	requireNotSignedBy(t, certA, m.cp, m.api, m.client)
	requireSignedBy(t, cpServerSigner(t, r), m.cp)
	assert.False(t, containsCertificate(apiBundleCerts(m), certA))
	assert.False(t, containsCertificate(serverBundleCerts(m), certA))
	// A data plane runs a certificate from A.
	rollDataPlane(t, r, "dp-ns", "dp", m)

	// cert-manager renews the CA in place.
	certB := rotateManualCA(t, r, "ca-b")

	// Phase 1: B is trusted next to A, A stays published even though the
	// pods had rolled onto the previous material.
	m1, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.NoError(t, err)
	require.Len(t, clientBundleCerts(m1), 2)
	assert.True(t, containsCertificate(clientBundleCerts(m1), certA))
	assert.True(t, containsCertificate(clientBundleCerts(m1), certB))
	assert.True(t, publishedClientCA(t, r).Equal(certA))
	assert.True(t, m1.rotating)

	// The bundle is lost mid-rotation: it is derived again from the signer
	// and the published CA.
	require.NoError(t, r.Delete(t.Context(), m1.clientBundle))
	rebuilt, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.NoError(t, err)
	require.Len(t, clientBundleCerts(rebuilt), 2)
	assert.True(t, containsCertificate(clientBundleCerts(rebuilt), certA))
	assert.True(t, containsCertificate(clientBundleCerts(rebuilt), certB))
	m1 = rebuilt

	// Until the pods run the new bundle, nothing moves.
	m1b, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.NoError(t, err)
	assert.Equal(t, m1.checksum(), m1b.checksum())
	assert.True(t, publishedClientCA(t, r).Equal(certA))

	// Phase 2: the pods trust A and B, B is published, A stays trusted
	// until the data plane runs a certificate from B.
	m2, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m1))
	require.NoError(t, err)
	assert.True(t, publishedClientCA(t, r).Equal(certB))
	assert.Equal(t, encodeCertificates([]*x509.Certificate{certA}), getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data[gwadminref.PreviousCAKey])
	require.Len(t, clientBundleCerts(m2), 2)
	assert.Equal(t, m1.checksum(), m2.checksum(), "publishing changes nothing mounted")
	assert.True(t, m2.rotating)
	assert.Contains(t, certificateCondition(gw).Message, "stays trusted until every data plane")
	assert.Contains(t, certificateCondition(gw).Message, "dp-ns/dp")
	assert.Equal(t, pki.MaxRenewalRequeue, m2.requeueAfter, "the data plane rollout triggers the next pass, not time")

	// Nothing moves while the data plane doesn't run it, whatever the time.
	now = now.Add(30 * 24 * time.Hour)
	m2b := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, clientBundleCerts(m2b), 2)

	// Trim: the data plane runs a certificate from B: A is no longer
	// trusted, nor recorded anywhere.
	rollDataPlane(t, r, "dp-ns", "dp", m2)
	m3, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m2))
	require.NoError(t, err)
	require.Len(t, clientBundleCerts(m3), 1)
	assert.True(t, clientBundleCerts(m3)[0].Equal(certB))
	assert.NotContains(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data, gwadminref.PreviousCAKey)
	assert.False(t, m3.rotating)
	cond := certificateCondition(gw)
	require.NotNil(t, cond)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, cond.Reason)

	// A new data plane, not rolled out yet, never brings A back.
	grantDataPlanes(t, r, "dp-ns")
	require.NoError(t, r.Create(t.Context(), dataPlaneDeploymentWith("dp-ns", "late", "", "", false)))
	m3b := ensure(t, r, gw, rolledDeployment(m3))
	require.Len(t, clientBundleCerts(m3b), 1)
	m3 = m3b

	// The certificates Kong Operator issues never noticed.
	requireSameLeaves(t, m, m3)
	require.Len(t, apiBundleCerts(m3), 1)
	require.Len(t, serverBundleCerts(m3), 1)
}

func TestEnsureTLS_ManualCP(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
	caA, caB := userCASecret(t, "public-a"), userCASecret(t, "public-b")
	issuerA, issuerB := caCert(t, caA), caCert(t, caB)
	src := userCPSecretFrom(t, caA, time.Time{}, 90*24*time.Hour, false)
	r := newReconciler(t, &now, gw, src)

	m, err := r.ensureTLS(t.Context(), gw, nil)
	require.NoError(t, err)
	assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey], "copied into the owned Secret")
	assert.Equal(t, src.Data[consts.CACRT], m.cp.Data[consts.CACRT], "the copy keeps the issuer of ca.crt")
	assert.True(t, metav1.IsControlledBy(m.cp, gw))
	assert.True(t, containsCertificate(serverBundleCerts(m), issuerA), "the data planes trust the user's issuer")
	assert.False(t, containsCertificate(clientBundleCerts(m), issuerA), "the cp listener never accepts it for clients")
	rollDataPlane(t, r, "dp-ns", "dp", m)

	// A certificate from another issuer is copied only once every data
	// plane runs a bundle trusting that issuer, whatever the pods do.
	renewed := userCPSecretFrom(t, caB, time.Time{}, 90*24*time.Hour, false)
	live := getSecret(t, r, userCPSecretName)
	live.Data = renewed.Data
	require.NoError(t, r.Update(t.Context(), live))

	pending := ensure(t, r, gw, rolledDeployment(m))
	assert.Equal(t, src.Data[corev1.TLSCertKey], pending.cp.Data[corev1.TLSCertKey])
	assert.True(t, containsCertificate(serverBundleCerts(pending), issuerA))
	assert.True(t, containsCertificate(serverBundleCerts(pending), issuerB))
	assert.True(t, pending.rotating)
	assert.Contains(t, certificateCondition(gw).Message, "moves to its new issuer once every data plane runs gw-cp-server-ca-bundle")
	assert.Contains(t, certificateCondition(gw).Message, "dp-ns/dp")

	// Still waiting while the data plane doesn't run it, whatever the time.
	now = now.Add(24 * time.Hour)
	still := ensure(t, r, gw, rolledDeployment(m))
	assert.Equal(t, src.Data[corev1.TLSCertKey], still.cp.Data[corev1.TLSCertKey])

	rollDataPlane(t, r, "dp-ns", "dp", pending)
	copied := ensure(t, r, gw, rollingDeployment(pending))
	assert.Equal(t, renewed.Data[corev1.TLSCertKey], copied.cp.Data[corev1.TLSCertKey])
	assert.True(t, containsCertificate(serverBundleCerts(copied), issuerA), "kept until the pods serve the new one")

	// Once the pods serve it, the previous issuer is dropped.
	trimmed := ensure(t, r, gw, rolledDeployment(copied))
	assert.False(t, containsCertificate(serverBundleCerts(trimmed), issuerA))
	assert.True(t, containsCertificate(serverBundleCerts(trimmed), issuerB))

	// A renewal from the same issuer is copied right away, once the data
	// plane runs the trimmed bundle, as its controller rolls it.
	rollDataPlane(t, r, "dp-ns", "dp", trimmed)
	sameIssuer := userCPSecretFrom(t, caB, now, 90*24*time.Hour, false)
	live = getSecret(t, r, userCPSecretName)
	live.Data = sameIssuer.Data
	require.NoError(t, r.Update(t.Context(), live))
	again := ensure(t, r, gw, rolledDeployment(trimmed))
	assert.Equal(t, sameIssuer.Data[corev1.TLSCertKey], again.cp.Data[corev1.TLSCertKey])
}

func TestEnsureTLS_StopsOnUserErrors(t *testing.T) {
	now := time.Now()

	t.Run("missing CA Secret", func(t *testing.T) {
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA("missing")})
		r := newReconciler(t, &now, gw)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefNotFound, certificateCondition(gw).Reason)
	})

	t.Run("CA Secret is not a CA", func(t *testing.T) {
		ca := userCASecret(t, "ca")
		leafSecret := userCPSecretFrom(t, ca, time.Time{}, time.Hour, false)
		ca.Data = map[string][]byte{
			corev1.TLSCertKey: leafSecret.Data[corev1.TLSCertKey], corev1.TLSPrivateKeyKey: leafSecret.Data[corev1.TLSPrivateKeyKey],
		}
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, ca)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, certificateCondition(gw).Reason)
	})

	t.Run("Secret not owned by the GWAdmin", func(t *testing.T) {
		gw := newGWAdmin(nil)
		foreign := &corev1.Secret{Name: "gw-api-tls", Namespace: testNamespace}
		r := newReconciler(t, &now, gw, foreign)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, certificateCondition(gw).Reason)
	})

	t.Run("operator namespace Secret not labelled with the GWAdmin UID", func(t *testing.T) {
		gw := newGWAdmin(nil)
		foreign := &corev1.Secret{
			Name: "gwadmin-gw-uid-api-ca", Namespace: testOperatorNamespace,
			Labels: map[string]string{consts.GWAdminUIDLabel: "other-uid"},
		}
		r := newReconciler(t, &now, gw, foreign)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, certificateCondition(gw).Reason)
		assert.Contains(t, certificateCondition(gw).Message, "gwadmin-gw-uid-api-ca")
	})

	t.Run("Secret still owned by a deleted GWAdmin of the same name", func(t *testing.T) {
		gw := newGWAdmin(nil)
		stale := &corev1.Secret{Name: "gw-api-tls", Namespace: testNamespace, OwnerReferences: []metav1.OwnerReference{{
			APIVersion: operatorv1alpha1.SchemeGroupVersion.String(), Kind: operatorv1alpha1.GWAdminKind,
			Name: "gw", UID: "previous-uid", Controller: new(true),
		}}}
		r := newReconciler(t, &now, gw, stale)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, conflictRequeue, stopRequeue(err))
		cond := certificateCondition(gw)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, cond.Reason)
		assert.Contains(t, cond.Message, "waiting for the garbage collector")
		assert.NotContains(t, cond.Message, "delete it, or rename it")
	})
}

func TestRollout(t *testing.T) {
	state := rolloutOf(nil)
	assert.False(t, state.complete, "no Deployment")
	assert.False(t, state.runs(consts.GWAdminTLSChecksumAnnotation, ""))
	assert.Equal(t, "the Deployment doesn't exist yet", state.description)

	for _, tc := range []struct {
		name            string
		mutate          func(d *appsv1.Deployment)
		wantComplete    bool
		wantTerminating bool
		wantDescription string
	}{
		{name: "every pod runs the template", wantComplete: true},
		{
			name:            "an old pod is still around",
			mutate:          func(d *appsv1.Deployment) { d.Status.Replicas = 2 },
			wantDescription: "waiting for the Deployment to roll out (1 of 1 pods updated, 1 available)",
		},
		{
			name:            "spec not observed yet",
			mutate:          func(d *appsv1.Deployment) { d.Generation = 2 },
			wantDescription: "waiting for the Deployment to roll out (1 of 1 pods updated, 1 available)",
		},
		{
			name:         "replicas defaults to 1",
			mutate:       func(d *appsv1.Deployment) { d.Spec.Replicas = nil },
			wantComplete: true,
		},
		{
			name: "replicas defaults to 1, two pods around",
			mutate: func(d *appsv1.Deployment) {
				d.Spec.Replicas = nil
				d.Status.Replicas, d.Status.UpdatedReplicas = 2, 2
			},
			wantDescription: "waiting for the Deployment to roll out (2 of 1 pods updated, 1 available)",
		},
		{
			name: "scaled to zero",
			mutate: func(d *appsv1.Deployment) {
				d.Spec.Replicas = new(int32(0))
				d.Status.Replicas, d.Status.UpdatedReplicas = 0, 0
			},
			wantComplete: true,
		},
		{
			name: "rollout in progress",
			mutate: func(d *appsv1.Deployment) {
				d.Spec.Replicas = new(int32(3))
				d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.AvailableReplicas = 3, 2, 3
			},
			wantDescription: "waiting for the Deployment to roll out (2 of 3 pods updated, 3 available)",
		},
		{
			name: "paused",
			mutate: func(d *appsv1.Deployment) {
				d.Spec.Paused = true
				d.Status.UpdatedReplicas = 0
			},
			wantDescription: "the Deployment is paused",
		},
		{
			name: "not progressing",
			mutate: func(d *appsv1.Deployment) {
				d.Status.UpdatedReplicas = 0
				d.Status.Conditions = []appsv1.DeploymentCondition{{
					Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
					Reason: "ProgressDeadlineExceeded", Message: "ReplicaSet gw-1 has timed out progressing.",
				}}
			},
			wantDescription: "the Deployment rollout is not progressing (ProgressDeadlineExceeded: ReplicaSet gw-1 has timed out progressing.)",
		},
		{
			name: "progressing",
			mutate: func(d *appsv1.Deployment) {
				d.Status.UpdatedReplicas = 0
				d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionTrue}}
			},
			wantDescription: "waiting for the Deployment to roll out (0 of 1 pods updated, 1 available)",
		},
		{
			name:            "an old pod is terminating",
			mutate:          func(d *appsv1.Deployment) { d.Status.TerminatingReplicas = new(int32(1)) },
			wantTerminating: true,
			wantDescription: "waiting for the old pods to terminate",
		},
		{
			name:         "no terminating pod counted",
			mutate:       func(d *appsv1.Deployment) { d.Status.TerminatingReplicas = new(int32(0)) },
			wantComplete: true,
		},
		{
			name:         "terminating pods not reported",
			mutate:       func(d *appsv1.Deployment) { d.Status.TerminatingReplicas = nil },
			wantComplete: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := rolledWith("sum", "trust")
			if tc.mutate != nil {
				tc.mutate(d)
			}
			state := rolloutOf(d)
			assert.Equal(t, tc.wantComplete, state.complete)
			assert.Equal(t, tc.wantTerminating, state.terminating)
			assert.Equal(t, tc.wantDescription, state.description)
			assert.Equal(t, tc.wantComplete, state.runs(consts.GWAdminTLSChecksumAnnotation, "sum"))
			assert.Equal(t, tc.wantComplete, state.runs(consts.GWAdminTrustChecksumAnnotation, "trust"))
			assert.False(t, state.runs(consts.GWAdminTLSChecksumAnnotation, "other"), "other material")
			assert.False(t, state.runs(consts.GWAdminTrustChecksumAnnotation, "sum"), "annotations are not mixed up")
		})
	}
}

// TestEnsureTLS_TerminatingPods checks that a rollout only waiting for old
// pods to terminate holds the next phase back.
func TestEnsureTLS_TerminatingPods(t *testing.T) {
	now := time.Now()
	caA := userCASecret(t, "ca-a")
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, caA)
	m := ensure(t, r, gw, nil)

	// A CA change: phase 1 runs, publishing waits for the old pod to go.
	certB := rotateManualCA(t, r, "ca-b")
	m1 := ensure(t, r, gw, rolledDeployment(m))
	require.Len(t, clientBundleCerts(m1), 2)

	d := rolledDeployment(m1)
	d.Status.TerminatingReplicas = new(int32(1))
	ensure(t, r, gw, d)
	assert.True(t, publishedClientCA(t, r).Equal(caCert(t, caA)))
	assert.Contains(t, certificateCondition(gw).Message, "waiting for the old pods to terminate")

	// Once gone, the new CA is published.
	ensure(t, r, gw, rolledDeployment(m1))
	assert.True(t, publishedClientCA(t, r).Equal(certB))
}

func TestEnsureTLS_AutomaticCARenewal(t *testing.T) {
	start := time.Now()
	now := start
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)

	m := ensure(t, r, gw, nil)
	clientA := publishedClientCA(t, r)
	apiA := apiSigner(t, r)
	serverA := cpServerSigner(t, r)
	caRenewal := renewalTime(clientA)
	require.Equal(t, caRenewal, renewalTime(apiA), "the certificate authorities are created together")
	require.Equal(t, caRenewal, renewalTime(serverA))
	assert.WithinDuration(t, start.Add(consts.GWAdminCATTL*2/3), caRenewal, 10*time.Minute, "renewed after about 8 months")
	rollDataPlane(t, r, "dp-ns", "dp", m)

	// Follow the requeues, the pods rolling right away, up to the CA
	// renewal: the leaves are renewed every 60 days under the same CAs, and
	// nothing else moves.
	var renewals int
	for passes := 0; ; passes++ {
		require.Less(t, passes, 5000, "too many passes")
		next := now.Add(m.requeueAfter)
		if !next.Before(caRenewal) {
			require.WithinDuration(t, caRenewal, next, 0, "requeued exactly at the CA renewal")
			now = next
			break
		}
		require.GreaterOrEqual(t, m.requeueAfter, time.Hour, "no hot loop")
		now = next
		prev := m
		m = ensure(t, r, gw, rolledDeployment(prev))
		if !leafCertificate(m.api).Equal(leafCertificate(prev.api)) {
			renewals++
		}
		require.True(t, publishedClientCA(t, r).Equal(clientA), "the CA is not renewed before it is due")
		require.True(t, apiSigner(t, r).Equal(apiA), "the API CA is not renewed before it is due")
		require.True(t, cpServerSigner(t, r).Equal(serverA), "the cp server CA is not renewed before it is due")
		require.Len(t, clientBundleCerts(m), 1)
		require.Len(t, apiBundleCerts(m), 1)
		require.Len(t, serverBundleCerts(m), 1)
		requireSignedBy(t, serverA, m.cp)
		require.False(t, m.rotating)
	}
	assert.InDelta(t, int(caRenewal.Sub(start)/(60*24*time.Hour)), renewals, 1, "leaves renewed every 60 days")

	// Phase 1: the three certificate authorities are renewed in the same
	// pass. The new cp client CA waits in the -next Secret: the old one stays
	// published. The leaves stay on the old CAs.
	m1 := ensure(t, r, gw, rolledDeployment(m))
	nextSecret := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca-next")
	clientB := caCert(t, nextSecret)
	require.False(t, clientB.Equal(clientA), "the CA is renewed")
	assert.Equal(t, string(gw.UID), nextSecret.Labels[consts.GWAdminUIDLabel])
	assert.True(t, publishedClientCA(t, r).Equal(clientA), "not published before the pods trust it")
	assert.WithinDuration(t, now.Add(-5*time.Minute).Truncate(time.Second), clientB.NotBefore, 0, "issued at the injected clock")
	require.Len(t, clientBundleCerts(m1), 2)
	assert.True(t, containsCertificate(clientBundleCerts(m1), clientA))
	assert.True(t, containsCertificate(clientBundleCerts(m1), clientB))
	apiB := apiSigner(t, r)
	require.False(t, apiB.Equal(apiA), "the API CA is renewed")
	require.Len(t, apiBundleCerts(m1), 2)
	assert.True(t, containsCertificate(apiBundleCerts(m1), apiA))
	assert.True(t, containsCertificate(apiBundleCerts(m1), apiB))
	assert.False(t, containsCertificate(apiBundleCerts(m1), clientB), "the bundles never mix")
	serverB := cpServerSigner(t, r)
	require.False(t, serverB.Equal(serverA), "the cp server CA is renewed")
	require.Len(t, serverBundleCerts(m1), 2)
	assert.True(t, containsCertificate(serverBundleCerts(m1), serverA))
	assert.True(t, containsCertificate(serverBundleCerts(m1), serverB))
	requireSignedBy(t, serverA, m1.cp)
	requireSignedBy(t, apiA, m1.api, m1.client)
	requireSameLeaves(t, m, m1)
	assert.True(t, m1.rotating)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCARotationInProgress, certificateCondition(gw).Reason)

	// Stable until the pods roll: the CAs aren't renewed twice, no leaf moves.
	for _, d := range []*appsv1.Deployment{rollingDeployment(m1), rolledDeployment(m)} {
		again := ensure(t, r, gw, d)
		assert.Equal(t, m1.checksum(), again.checksum())
		assert.True(t, caCert(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca-next")).Equal(clientB), "the CA is renewed once")
		assert.True(t, publishedClientCA(t, r).Equal(clientA))
		assert.True(t, apiSigner(t, r).Equal(apiB), "the API CA is renewed once")
		assert.True(t, cpServerSigner(t, r).Equal(serverB), "the cp server CA is renewed once")
	}

	// Phase 2: the pods trust both, the new cp client CA is published and
	// the API leaves move. The cp server certificate waits for the data
	// plane to run the bundle trusting the new cp server CA.
	m2 := ensure(t, r, gw, rolledDeployment(m1))
	published := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")
	assert.True(t, caCert(t, published).Equal(clientB))
	assert.Equal(t, nextSecret.Data[corev1.TLSPrivateKeyKey], published.Data[corev1.TLSPrivateKeyKey])
	assert.Equal(t, encodeCertificates([]*x509.Certificate{clientA}), published.Data[gwadminref.PreviousCAKey])
	assert.Equal(t, now.UTC().Format(time.RFC3339), published.Annotations[caPublishedAtAnnotation])
	err := r.Get(t.Context(), types.NamespacedName{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca-next"}, &corev1.Secret{})
	assert.True(t, apierrors.IsNotFound(err), "%v", err)
	require.Len(t, clientBundleCerts(m2), 2)
	require.Len(t, apiBundleCerts(m2), 2)
	requireSignedBy(t, apiB, m2.api, m2.client)
	requireSignedBy(t, serverA, m2.cp)
	assert.True(t, m2.rotating)

	// Stable until the pods roll again.
	for _, d := range []*appsv1.Deployment{rollingDeployment(m2), rolledDeployment(m1)} {
		again := ensure(t, r, gw, d)
		assert.Equal(t, m2.checksum(), again.checksum())
		assert.Len(t, clientBundleCerts(again), 2)
		assert.Len(t, apiBundleCerts(again), 2)
	}

	// Trim: the old API CA is dropped. The old cp client CA stays, and
	// the cp server certificate waits, until the data plane rolls out.
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, apiBundleCerts(m3), 1)
	assert.True(t, apiBundleCerts(m3)[0].Equal(apiB))
	require.Len(t, clientBundleCerts(m3), 2)
	require.Len(t, serverBundleCerts(m3), 2)
	requireSignedBy(t, serverA, m3.cp)
	assert.True(t, m3.rotating)
	assert.Contains(t, certificateCondition(gw).Message, "stays trusted until every data plane")
	assert.Contains(t, certificateCondition(gw).Message, "moves to its new issuer once every data plane")

	// The data plane runs the new bundle and a certificate from the new cp
	// client CA: the old cp client CA is dropped, and the cp server
	// certificate moves to the new cp server CA.
	rollDataPlane(t, r, "dp-ns", "dp", m3)
	m4 := ensure(t, r, gw, rolledDeployment(m3))
	require.Len(t, clientBundleCerts(m4), 1)
	assert.True(t, clientBundleCerts(m4)[0].Equal(clientB))
	requireSignedBy(t, serverB, m4.cp)
	require.Len(t, serverBundleCerts(m4), 2, "the old issuer stays until the pods serve the new certificate")

	// Once the pods serve it, the old cp server CA is dropped.
	m5 := ensure(t, r, gw, rolledDeployment(m4))
	require.Len(t, serverBundleCerts(m5), 1)
	assert.True(t, serverBundleCerts(m5)[0].Equal(serverB))
	assert.False(t, m5.rotating)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonCertificateProvisioned, certificateCondition(gw).Reason)

	// Done: nothing moves any more.
	m6 := ensure(t, r, gw, rolledDeployment(m5))
	assert.Equal(t, m5.checksum(), m6.checksum())
	assert.Equal(t, m5.serverBundle.Data, m6.serverBundle.Data)
	assert.True(t, publishedClientCA(t, r).Equal(clientB))
	assert.Equal(t, pki.MaxRenewalRequeue, m6.requeueAfter)
}

func TestEnsureTLS_LeafRenewalIsStable(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)

	m := ensure(t, r, gw, nil)

	// Past two thirds of the leaf validity: one pass renews every leaf.
	now = now.Add(consts.GWAdminLeafTTL * 3 / 4)
	renewed := ensure(t, r, gw, rolledDeployment(m))
	for i, l := range leaves(renewed) {
		assert.False(t, leafCertificate(l).Equal(leafCertificate(leaves(m)[i])), "%s is renewed", l.Name)
	}
	newLeaf := leafCertificate(renewed.api)
	assert.WithinDuration(t, now.Truncate(time.Minute).Add(-5*time.Minute), newLeaf.NotBefore, 0, "issued at the injected clock")
	assert.Equal(t, pki.MaxRenewalRequeue, renewed.requeueAfter, "the renewed leaves are due in 60 days")

	// The next pass doesn't renew them again.
	again := ensure(t, r, gw, rolledDeployment(renewed))
	requireSameLeaves(t, renewed, again)
	assert.Equal(t, renewed.checksum(), again.checksum())

	// Shortly before the renewal is due, the requeue lands on it exactly.
	now = renewalTime(newLeaf).Add(-90 * time.Minute)
	before := ensure(t, r, gw, rolledDeployment(again))
	requireSameLeaves(t, renewed, before)
	assert.Equal(t, renewalTime(newLeaf).Sub(now), before.requeueAfter)
	assert.Equal(t, 90*time.Minute, before.requeueAfter)
}

// TestEnsureTLS_ManualCAExpiryWarning checks that a Manual CA close to its
// expiry is reported: once less than a sixth of its validity is left, a
// warning is added to the condition.
func TestEnsureTLS_ManualCAExpiryWarning(t *testing.T) {
	now := time.Now()
	ca := userCASecretValidAt(t, "short-lived", now, 6*24*time.Hour)
	notAfter := caCert(t, ca).NotAfter
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, ca)

	m := ensure(t, r, gw, nil)
	assert.NotContains(t, certificateCondition(gw).Message, "expires at")
	warnAt := warningTime(caCert(t, ca))
	assert.LessOrEqual(t, m.requeueAfter, warnAt.Sub(now))

	now = warnAt.Add(-time.Minute)
	m = ensure(t, r, gw, rolledDeployment(m))
	assert.NotContains(t, certificateCondition(gw).Message, "expires at")
	assert.Equal(t, time.Minute, m.requeueAfter, "looks again when the warning starts")

	now = warnAt.Add(time.Minute)
	m = ensure(t, r, gw, rolledDeployment(m))
	cond := certificateCondition(gw)
	assert.Equal(t, metav1.ConditionTrue, cond.Status, "a warning stops nothing")
	assert.Contains(t, cond.Message, `CA Secret "user-ca" expires at `+notAfter.UTC().Format(time.RFC3339))
	assert.Equal(t, notAfter.Sub(now), m.requeueAfter, "looks again at its expiry")
}

func TestEnsureTLS_ManualCAValidity(t *testing.T) {
	now := time.Now()

	t.Run("expired", func(t *testing.T) {
		ca := userCASecretValidAt(t, "expired", now.Add(-2*time.Hour), time.Hour)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, ca)

		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, pki.MaxRenewalRequeue, stopRequeue(err))
		cond := certificateCondition(gw)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cond.Reason)
		assert.Contains(t, cond.Message, "expired")
	})

	t.Run("not yet valid", func(t *testing.T) {
		now := now
		ca := userCASecretValidAt(t, "future", now.Add(time.Hour), 24*time.Hour)
		notBefore := caCert(t, ca).NotBefore
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, ca)

		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		// Accepted up to 5 minutes before its NotBefore, for clock skew.
		assert.InDelta(t, notBefore.Add(-clockSkew).Sub(now), stopRequeue(err), float64(time.Second))
		assert.InDelta(t, 50*time.Minute, stopRequeue(err), float64(time.Second))
		cond := certificateCondition(gw)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cond.Reason)
		assert.Contains(t, cond.Message, "not valid before")

		// Once valid, it is trusted and published.
		now = notBefore
		m := ensure(t, r, gw, nil)
		assert.True(t, containsCertificate(clientBundleCerts(m), caCert(t, ca)))
		assert.True(t, publishedClientCA(t, r).Equal(caCert(t, ca)))
	})

	t.Run("far in the future: requeue capped", func(t *testing.T) {
		now := time.Now()
		ca := userCASecretValidAt(t, "far", now.Add(72*time.Hour), 24*time.Hour)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, ca)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, pki.MaxRenewalRequeue, stopRequeue(err))
	})

	for _, tc := range []struct {
		name    string
		key     crypto.Signer
		mutate  func(*x509.Certificate)
		wantErr string
	}{
		{
			name:    "can't sign certificates",
			mutate:  func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageDigitalSignature },
			wantErr: "can't sign certificates",
		},
		{name: "no key usage at all is accepted", mutate: func(c *x509.Certificate) { c.KeyUsage = 0 }},
		{
			name:    "RSA key too short",
			key:     rsaKey(t, 1024),
			wantErr: "1024 bits RSA key is too short",
		},
		{name: "RSA 2048 is accepted", key: rsaKey(t, 2048)},
		{
			name:    "extended key usage excluding client authentication",
			mutate:  func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} },
			wantErr: "excludes client authentication",
		},
		{
			name:   "extended key usage including client authentication is accepted",
			mutate: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} },
		},
		{
			name:   "any extended key usage is accepted",
			mutate: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageAny} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ca := selfSignedCA(t, now, tc.key, tc.mutate)
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
			r := newReconciler(t, &now, gw, ca)

			_, err := r.ensureTLS(t.Context(), gw, nil)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errStop)
			cond := certificateCondition(gw)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, cond.Reason)
			assert.Contains(t, cond.Message, tc.wantErr)
		})
	}
}

func TestEnsureTLS_ManualCARefusesOperatorManagedSecrets(t *testing.T) {
	now := time.Now()

	t.Run("the GWAdmin's own Automatic CA", func(t *testing.T) {
		gw := newGWAdmin(nil)
		r := newReconciler(t, &now, gw)
		ensure(t, r, gw, nil)
		owned := getSecret(t, r, "gw-cp-tls")

		gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA("gw-cp-tls")}
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefOperatorManaged, certificateCondition(gw).Reason)
		assert.Equal(t, owned.Data, getSecret(t, r, "gw-cp-tls").Data, "not deleted nor modified")
	})

	t.Run("a Secret labeled as automatically provisioned", func(t *testing.T) {
		ca := userCASecret(t, "labeled")
		ca.Labels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		r := newReconciler(t, &now, gw, ca)

		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefOperatorManaged, certificateCondition(gw).Reason)
	})
}

func TestEnsureTLS_SwitchToManualCAPublishesItOnceTrusted(t *testing.T) {
	// pass runs ensureTLS like Reconcile does.
	pass := func(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin, d *appsv1.Deployment) *tlsMaterial {
		t.Helper()
		m := ensure(t, r, gw, d)
		setStatus(gw, r.ClusterDomain)
		return m
	}

	t.Run("owned", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		userCA := userCASecret(t, "user")
		r := newReconciler(t, &now, gw, userCA)
		m := pass(t, r, gw, nil)
		autoCA := publishedClientCA(t, r)
		rollDataPlane(t, r, "dp-ns", "dp", m)

		gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}

		// Phase 1: the Manual CA is added to the bundle. The pods don't
		// trust it yet: the Automatic CA stays published.
		m1 := pass(t, r, gw, rolledDeployment(m))
		require.Len(t, clientBundleCerts(m1), 2)
		assert.True(t, containsCertificate(clientBundleCerts(m1), autoCA))
		assert.True(t, containsCertificate(clientBundleCerts(m1), caCert(t, userCA)))
		assert.True(t, publishedClientCA(t, r).Equal(autoCA), "the Manual CA is not published before the pods trust it")

		// The Deployment carries the new trust bundle, but its pods didn't
		// roll yet: still nothing.
		for _, d := range []*appsv1.Deployment{rolledDeployment(m), rollingDeployment(m1)} {
			again := pass(t, r, gw, d)
			assert.Equal(t, m1.checksum(), again.checksum())
			assert.True(t, publishedClientCA(t, r).Equal(autoCA))
		}

		// The pods trust the Manual CA: it is published, no certificate
		// Kong Operator issues moves.
		m2 := pass(t, r, gw, rolledDeployment(m1))
		assert.True(t, publishedClientCA(t, r).Equal(caCert(t, userCA)))
		assert.True(t, isManualCopy(getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")))
		assert.Equal(t, userCA.Data[corev1.TLSPrivateKeyKey], getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data[corev1.TLSPrivateKeyKey])
		requireSameLeaves(t, m, m2)
		require.Len(t, clientBundleCerts(m2), 2)

		// The previous CA stays trusted until the data plane runs a
		// certificate from the Manual CA.
		m3 := pass(t, r, gw, rolledDeployment(m2))
		require.Len(t, clientBundleCerts(m3), 2)
		assert.True(t, m3.rotating)
		assert.Contains(t, certificateCondition(gw).Message, "stays trusted until every data plane")

		rollDataPlane(t, r, "dp-ns", "dp", m3)
		m4 := pass(t, r, gw, rolledDeployment(m3))
		require.Len(t, clientBundleCerts(m4), 1)
		assert.True(t, clientBundleCerts(m4)[0].Equal(caCert(t, userCA)))
		assert.False(t, m4.rotating)
	})

	t.Run("without a Deployment, the switch is immediate", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		userCA := userCASecret(t, "user")
		r := newReconciler(t, &now, gw, userCA)
		pass(t, r, gw, nil)
		autoCA := publishedClientCA(t, r)

		gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}
		m1 := pass(t, r, gw, nil)
		assert.True(t, publishedClientCA(t, r).Equal(caCert(t, userCA)), "no pod to wait for")
		// No data plane to wait for either: the previous CA is dropped.
		require.Len(t, clientBundleCerts(m1), 1)
		assert.False(t, containsCertificate(clientBundleCerts(m1), autoCA))
		assert.NotContains(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data, gwadminref.PreviousCAKey)

		m2 := pass(t, r, gw, rolledDeployment(m1))
		require.Len(t, clientBundleCerts(m2), 1)
		assert.True(t, clientBundleCerts(m2)[0].Equal(caCert(t, userCA)))
	})

	t.Run("a Deployment with the new bundle in its template but not rolled out", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(nil)
		userCA := userCASecret(t, "user")
		r := newReconciler(t, &now, gw, userCA)
		m := pass(t, r, gw, nil)
		autoCA := publishedClientCA(t, r)

		gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)}
		m1 := pass(t, r, gw, rolledDeployment(m))
		// The pods of the previous template run, the new template doesn't
		// progress: nothing is published, whatever the pass.
		d := rollingDeployment(m1)
		d.Status.Conditions = []appsv1.DeploymentCondition{{
			Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded",
		}}
		for range 3 {
			pass(t, r, gw, d)
			assert.True(t, publishedClientCA(t, r).Equal(autoCA))
		}
		assert.Contains(t, certificateCondition(gw).Message, "ProgressDeadlineExceeded")
	})

	t.Run("foreign Secret with the same name is left alone", func(t *testing.T) {
		now := time.Now()
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
		foreign := &corev1.Secret{Name: "gwadmin-gw-uid-cp-client-ca", Namespace: testOperatorNamespace, Data: map[string][]byte{"k": []byte("v")}}
		r := newReconciler(t, &now, gw, userCASecret(t, "user"), foreign)
		_, err := r.ensureTLS(t.Context(), gw, nil)
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, certificateCondition(gw).Reason)
		assert.Equal(t, foreign.Data, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca").Data)
	})
}

func TestEnsureTLS_SecretHiddenFromTheCache(t *testing.T) {
	for _, key := range []types.NamespacedName{
		{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-client-ca"},
		{Namespace: testNamespace, Name: "gw-cp-client-ca-bundle"},
		{Namespace: testNamespace, Name: "gw-cp-server-ca-bundle"},
		{Namespace: testNamespace, Name: "gw-api-ca-bundle"},
		{Namespace: testNamespace, Name: "gw-api-tls"},
		{Namespace: testNamespace, Name: "gw-cp-tls"},
		{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-api-ca"},
		{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-cp-server-ca"},
		{Namespace: testOperatorNamespace, Name: "gwadmin-gw-uid-operator-client-tls"},
	} {
		t.Run(key.String(), func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			// Without the operator's secret label, the Secret is not cached:
			// Get misses it, Create runs into it.
			foreign := &corev1.Secret{Name: key.Name, Namespace: key.Namespace, Data: map[string][]byte{"k": []byte("v")}}
			base := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, foreign).Build()
			r := reconcilerWith(hidingClient(base, key.Name), &now)

			_, err := r.ensureTLS(t.Context(), gw, nil)
			require.ErrorIs(t, err, errStop)
			assert.Equal(t, conflictRequeue, stopRequeue(err))
			cond := certificateCondition(gw)
			assert.Equal(t, metav1.ConditionFalse, cond.Status)
			assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretConflict, cond.Reason)
			assert.Contains(t, cond.Message, key.Name)

			live := &corev1.Secret{}
			require.NoError(t, base.Get(t.Context(), client.ObjectKeyFromObject(foreign), live))
			assert.Equal(t, foreign.Data, live.Data, "left alone")
			assert.Empty(t, live.OwnerReferences)
		})
	}
}

func TestEnsureTLS_ManualCPToAutomatic(t *testing.T) {
	now := time.Now()
	src, userIssuer := userCPSecret(t, time.Time{}, true)
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
	r := newReconciler(t, &now, gw, src)

	m := ensure(t, r, gw, nil)
	signer := cpServerSigner(t, r)
	assert.Equal(t, certificateSourceManual, m.cp.Annotations[certificateSourceAnnotation])
	assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey])
	// The cp server CA is trusted all along, next to the user's issuer.
	require.Len(t, serverBundleCerts(m), 2)
	assert.True(t, containsCertificate(serverBundleCerts(m), signer))
	assert.True(t, containsCertificate(serverBundleCerts(m), userIssuer))
	assert.False(t, containsCertificate(clientBundleCerts(m), userIssuer), "never trusted for clients")
	assert.False(t, m.rotating)

	// Back to Automatic while a data plane doesn't run the current bundle:
	// the copy stays.
	grantDataPlanes(t, r, "dp-ns")
	require.NoError(t, r.Create(t.Context(), dataPlaneDeploymentWith("dp-ns", "dp", "stale", "", true)))
	gw.Spec.TLS = nil
	early := ensure(t, r, gw, rolledDeployment(m))
	assert.Equal(t, src.Data[corev1.TLSCertKey], early.cp.Data[corev1.TLSCertKey], "gated on the data planes")
	assert.True(t, early.rotating)
	assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp-ns/dp doesn't run it yet")

	// Once it runs the current bundle, which trusted the cp server CA all
	// along, the cp certificate is issued by Kong Operator, whatever the
	// pods do, and the manual marker is gone.
	rollDataPlane(t, r, "dp-ns", "dp", early)
	m2 := ensure(t, r, gw, rollingDeployment(m))
	requireSignedBy(t, signer, m2.cp)
	assert.NotContains(t, m2.cp.Annotations, certificateSourceAnnotation)
	assert.NotContains(t, getSecret(t, r, "gw-cp-tls").Annotations, certificateSourceAnnotation)
	assert.True(t, containsCertificate(serverBundleCerts(m2), userIssuer), "kept while the pods may still serve the copy")

	// Once the pods serve it, the user's issuer is no longer trusted.
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, serverBundleCerts(m3), 1)
	assert.True(t, serverBundleCerts(m3)[0].Equal(signer))
	assert.False(t, m3.rotating)

	m4 := ensure(t, r, gw, rolledDeployment(m3))
	assert.Equal(t, m3.checksum(), m4.checksum())

	t.Run("without data planes, the switch is immediate", func(t *testing.T) {
		now := time.Now()
		src, _ := userCPSecret(t, time.Time{}, true)
		gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
		r := newReconciler(t, &now, gw, src)
		m := ensure(t, r, gw, nil)
		signer := cpServerSigner(t, r)

		gw.Spec.TLS = nil
		m = ensure(t, r, gw, rollingDeployment(m))
		requireSignedBy(t, signer, m.cp)
		assert.NotContains(t, m.cp.Annotations, certificateSourceAnnotation)
	})
}

func TestEnsureTLS_ManualCPValidityPeriod(t *testing.T) {
	// Far from the wall clock: only the injected clock can make it valid.
	issuedAt := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Minute)
	src, _ := userCPSecret(t, issuedAt, false)
	notBefore, notAfter := leafCertificate(src).NotBefore, leafCertificate(src).NotAfter
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{CP: manualCP()})
	now := issuedAt.Add(-time.Hour)
	r := newReconciler(t, &now, gw, src)

	// A NotBefore up to 5 minutes ahead is tolerated: requeue for that moment.
	_, err := r.ensureTLS(t.Context(), gw, nil)
	require.ErrorIs(t, err, errStop)
	assert.Equal(t, notBefore.Add(-5*time.Minute).Sub(now), stopRequeue(err))
	assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, certificateCondition(gw).Reason)
	assert.Contains(t, certificateCondition(gw).Message, "not valid before")

	t.Run("tolerated 5 minutes early", func(t *testing.T) {
		now = notBefore.Add(-5 * time.Minute)
		m := ensure(t, r, gw, nil)
		assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey], "copied once valid")
	})

	now = notBefore
	m := ensure(t, r, gw, nil)
	assert.Equal(t, src.Data[corev1.TLSCertKey], m.cp.Data[corev1.TLSCertKey], "copied once valid")
	assert.Equal(t, metav1.ConditionTrue, certificateCondition(gw).Status)

	now = notAfter.Add(time.Second)
	_, err = r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.ErrorIs(t, err, errStop)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, certificateCondition(gw).Reason)
	assert.Contains(t, certificateCondition(gw).Message, "expired at")
}

// TestEnsureTLS_APIBundleTrimWaitsForLeafRenewal checks that the previous API
// CA is dropped only once the pods run the certificates issued last, when a
// renewal comes right after the leaves moved to the new API CA.
func TestEnsureTLS_APIBundleTrimWaitsForLeafRenewal(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := ensure(t, r, gw, nil)

	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
	m1 := ensure(t, r, gw, rolledDeployment(m))
	apiB := apiSigner(t, r)
	require.Len(t, apiBundleCerts(m1), 2)
	m2 := ensure(t, r, gw, rolledDeployment(m1))
	requireSignedBy(t, apiB, m2.api, m2.client)
	require.Len(t, apiBundleCerts(m2), 2)

	// The pods run the new leaves, and the leaves are due for renewal in the
	// same pass: they are renewed, and the old CA is dropped only once the
	// pods run the renewed ones.
	now = now.Add(consts.GWAdminLeafTTL * 3 / 4)
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	assert.False(t, leafCertificate(m3.api).Equal(leafCertificate(m2.api)), "renewed")
	require.Len(t, apiBundleCerts(m3), 2, "the trim doesn't run in the same pass")
	assert.True(t, m3.rotating)

	m4 := ensure(t, r, gw, rolledDeployment(m3))
	require.Len(t, apiBundleCerts(m4), 1)
	assert.True(t, apiBundleCerts(m4)[0].Equal(apiB))
	assert.True(t, leafCertificate(m4.api).Equal(leafCertificate(m3.api)))
}

func TestEnsureTLS_ManualCPDuringCARotation(t *testing.T) {
	now := time.Now()
	caA := userCASecret(t, "ca-a")
	src, userIssuer := userCPSecret(t, time.Time{}, true)
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName), CP: manualCP()})
	r := newReconciler(t, &now, gw, caA, src)
	m := ensure(t, r, gw, nil)
	assert.False(t, m.rotating)
	rollDataPlane(t, r, "dp-ns", "dp", m)

	certB := rotateManualCA(t, r, "ca-b")

	m1 := ensure(t, r, gw, rolledDeployment(m))
	require.Len(t, clientBundleCerts(m1), 2)
	m2 := ensure(t, r, gw, rolledDeployment(m1))
	// The user's certificate authority signs none of the certificates Kong
	// Operator issues: the API ones stay on their own.
	requireSignedBy(t, apiSigner(t, r), m2.api, m2.client)
	requireSameLeaves(t, m, m2)
	// Only the data plane keeps A trusted, until it runs a certificate from B.
	require.Len(t, clientBundleCerts(ensure(t, r, gw, rolledDeployment(m2))), 2)
	rollDataPlane(t, r, "dp-ns", "dp", m2)
	m3 := ensure(t, r, gw, rolledDeployment(m2))
	require.Len(t, clientBundleCerts(m3), 1)
	assert.True(t, clientBundleCerts(m3)[0].Equal(certB))
	assert.False(t, m3.rotating)
	assert.Equal(t, src.Data[corev1.TLSCertKey], m3.cp.Data[corev1.TLSCertKey])
	assert.False(t, containsCertificate(clientBundleCerts(m3), userIssuer))
	assert.True(t, containsCertificate(serverBundleCerts(m3), userIssuer))
}

func TestEnsureTLS_OwnedSecretsMetadata(t *testing.T) {
	now := time.Now()
	src, _ := userCPSecret(t, time.Time{}, false)

	for _, tc := range []struct {
		name         string
		tls          *operatorv1alpha1.GWAdminTLS
		wantManualCP bool
	}{
		{name: "automatic"},
		{name: "manual cp", tls: &operatorv1alpha1.GWAdminTLS{CP: manualCP()}, wantManualCP: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newGWAdmin(tc.tls)
			r := newReconciler(t, &now, gw, src.DeepCopy())
			ensure(t, r, gw, nil)

			for _, name := range ownedSecretNames(gw) {
				s := getSecret(t, r, name)
				assert.True(t, metav1.IsControlledBy(s, gw), name)
				assert.Equal(t, consts.SecretProvisioningAutomaticLabelValue, s.Labels[consts.SecretProvisioningLabelKey], name)
				assert.Equal(t, "true", s.Labels["konghq.com/secret"], name)
				assert.Equal(t, "gw", s.Labels[consts.GatewayOperatorManagedByNameLabel], name)
				assert.Equal(t, "gw-uid", s.Labels[consts.GWAdminUIDLabel], name)
				assert.NotContains(t, s.Labels, consts.GatewayOperatorManagedByNamespaceLabel, name)
				if name == "gw-cp-tls" && tc.wantManualCP {
					assert.Equal(t, certificateSourceManual, s.Annotations[certificateSourceAnnotation], name)
				} else {
					assert.NotContains(t, s.Annotations, certificateSourceAnnotation, name)
				}
			}

			for _, name := range privateSecretNames(gw) {
				if name == cpClientCANextSecretName(gw) {
					continue // Only during a renewal.
				}
				s := getPrivateSecret(t, r, name)
				err := r.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: name}, &corev1.Secret{})
				assert.True(t, apierrors.IsNotFound(err), "%s is not in the GWAdmin namespace", name)
				assert.Empty(t, s.OwnerReferences, name)
				assert.Equal(t, "gw-uid", s.Labels[consts.GWAdminUIDLabel], name)
				assert.Equal(t, "gw", s.Labels[consts.GatewayOperatorManagedByNameLabel], name)
				assert.Equal(t, testNamespace, s.Labels[consts.GatewayOperatorManagedByNamespaceLabel], name)
				assert.Equal(t, "true", s.Labels["konghq.com/secret"], name)
				assert.Equal(t, consts.SecretProvisioningAutomaticLabelValue, s.Labels[consts.SecretProvisioningLabelKey], name)
			}
		})
	}
}

func TestStopError(t *testing.T) {
	err := &stopError{after: time.Minute}
	assert.Equal(t, errStop.Error(), err.Error())
	assert.ErrorIs(t, err, errStop)
}

func TestAnchorsOf_SkipsWhatIsNoCA(t *testing.T) {
	ca := userCASecret(t, "issuer")
	src := userCPSecretFrom(t, ca, time.Time{}, 90*24*time.Hour, false)
	// ca.crt also holds a private key and the leaf itself: neither is an
	// anchor.
	src.Data[consts.CACRT] = append(append(src.Data[corev1.TLSPrivateKeyKey], src.Data[corev1.TLSCertKey]...), src.Data[consts.CACRT]...)
	anchors := anchorsOf(src, time.Now())
	require.Len(t, anchors, 1)
	assert.True(t, anchors[0].Equal(caCert(t, ca)))
}
