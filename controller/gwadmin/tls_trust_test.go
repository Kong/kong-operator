package gwadmin

import (
	"crypto/x509"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// tRogue returns a certificate authority none of the domains should ever
// trust, and its Secret.
func tRogue(t *testing.T) (*corev1.Secret, *x509.Certificate) {
	t.Helper()
	s := userCASecret(t, "rogue")
	return s, caCert(t, s)
}

// tTamperBundle adds rogue to the ca.crt of the named bundle Secret, as
// whoever can write Secrets in the namespace of the GWAdmin could, and
// forges annotations claiming it was published.
func tTamperBundle(t *testing.T, r *Reconciler, name string, rogue *x509.Certificate) {
	t.Helper()
	live := getSecret(t, r, name)
	live.Data[consts.CACRT] = append(live.Data[consts.CACRT], encodeCertificates([]*x509.Certificate{rogue})...)
	if live.Annotations == nil {
		live.Annotations = map[string]string{}
	}
	live.Annotations["konghq.com/gw-admin-published-cas"] = `{"` + pki.Fingerprint(rogue) + `":"previous"}`
	require.NoError(t, r.Update(t.Context(), live))
}

// tRequireNeverTrusted fails when any bundle of m trusts rogue.
func tRequireNeverTrusted(t *testing.T, m *tlsMaterial, rogue *x509.Certificate) {
	t.Helper()
	assert.False(t, containsCertificate(apiBundleCerts(m), rogue), "the API bundle trusts the rogue CA")
	assert.False(t, containsCertificate(serverBundleCerts(m), rogue), "the cp server bundle trusts the rogue CA")
	assert.False(t, containsCertificate(clientBundleCerts(m), rogue), "the cp client bundle trusts the rogue CA")
}

// TestEnsureTLS_TamperedBundlesAreRewritten checks that what a bundle Secret
// in the namespace of the GWAdmin holds is never trusted again: a CA added to
// it, with forged annotations, is gone on the next pass, also mid-rotation
// while a data plane holds the rotations back.
func TestEnsureTLS_TamperedBundlesAreRewritten(t *testing.T) {
	_, rogue := tRogue(t)
	for _, name := range []string{"gw-api-ca-bundle", "gw-cp-server-ca-bundle", "gw-cp-client-ca-bundle"} {
		t.Run(name+", steady", func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			r := newReconciler(t, &now, gw)
			m := dSteady(t, r, gw)
			tTamperBundle(t, r, name, rogue)
			for range 3 {
				m = ensure(t, r, gw, rolledDeployment(m))
				tRequireNeverTrusted(t, m, rogue)
			}
		})

		t.Run(name+", mid-rotation with a stalled data plane", func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			r := newReconciler(t, &now, gw)
			m := dSteady(t, r, gw)
			rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
			dRotate(gw)
			m = ensure(t, r, gw, rollingDeployment(m))
			setDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, nil, false)
			tTamperBundle(t, r, name, rogue)
			for range 3 {
				m = ensure(t, r, gw, rollingDeployment(m))
				tRequireNeverTrusted(t, m, rogue)
			}
			for range 3 {
				m = ensure(t, r, gw, rolledDeployment(m))
				tRequireNeverTrusted(t, m, rogue)
			}
		})
	}
}

// TestEnsureTLS_RogueChainInALeafIsNeverTrusted checks that a CA carried in
// the chain of a certificate Secret in the namespace of the GWAdmin is never
// trusted: the certificate is issued again, from the CA of its domain.
func TestEnsureTLS_RogueChainInALeafIsNeverTrusted(t *testing.T) {
	rogueSecret, rogue := tRogue(t)
	for _, tc := range []struct {
		name string
		spec leafSpec
	}{
		{name: "gw-api-tls", spec: serverLeafSpec("gw-api", testNamespace, "cluster.local")},
		{name: "gw-cp-tls", spec: serverLeafSpec("gw-cp", testNamespace, "cluster.local")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			r := newReconciler(t, &now, gw)
			m := dSteady(t, r, gw)

			certPEM, keyPEM, err := pki.IssueCertificate(rogueSecret, pki.CertRequest{
				CommonName: tc.spec.commonName, DNSNames: tc.spec.dnsNames, Usages: tc.spec.usages, TTL: time.Hour, Now: now,
			})
			require.NoError(t, err)
			live := getSecret(t, r, tc.name)
			live.Data = map[string][]byte{
				corev1.TLSCertKey:       append(certPEM, encodeCertificates([]*x509.Certificate{rogue})...),
				corev1.TLSPrivateKeyKey: keyPEM,
			}
			require.NoError(t, r.Update(t.Context(), live))

			for range 3 {
				m = ensure(t, r, gw, rolledDeployment(m))
				tRequireNeverTrusted(t, m, rogue)
			}
			requireSignedBy(t, apiSigner(t, r), m.api)
			requireSignedBy(t, cpServerSigner(t, r), m.cp)
		})
	}
}

// TestEnsureTLS_MissingClientCertificateDuringAPIRotation checks that the
// operator client certificate, lost while the API CA rotates and the rollout
// stalls, is issued at once from the new API CA: the new pods only turn
// Ready once provisioned with it.
func TestEnsureTLS_MissingClientCertificateDuringAPIRotation(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := dSteady(t, r, gw)
	oldAPI := apiSigner(t, r)

	dRotate(gw)
	m = ensure(t, r, gw, rolledDeployment(m))
	newAPI := apiSigner(t, r)
	require.False(t, newAPI.Equal(oldAPI))
	requireSignedBy(t, oldAPI, m.client)

	require.NoError(t, r.Delete(t.Context(), getPrivateSecret(t, r, gwadminref.OperatorClientTLSSecretName(gw))))
	stalled := rollingDeployment(m)
	m = ensure(t, r, gw, stalled)
	require.NotNil(t, m.client)
	requireSignedBy(t, newAPI, m.client)
}

// TestEnsureTLS_AStoppedDomainDoesntHoldTheOthersBack checks that, with the
// Manual cp client CA Secret gone, the API and cp server domains still renew,
// and the stop is reported with the earlier requeue they need.
func TestEnsureTLS_AStoppedDomainDoesntHoldTheOthersBack(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
	r := newReconciler(t, &now, gw, userCASecret(t, "user"))
	m := dSteady(t, r, gw)
	require.NoError(t, r.Delete(t.Context(), getSecret(t, r, userCASecretName)))

	// An hour before the leaves are due: the stop asks for a day, the leaves
	// for an hour.
	now = renewalTime(leafCertificate(m.cp)).Add(-time.Hour)
	_, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.ErrorIs(t, err, errStop)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefNotFound, certificateCondition(gw).Reason)
	assert.Equal(t, time.Hour, stopRequeue(err))

	// Due: the cp server and API certificates are renewed regardless.
	cpBefore, apiBefore := leafCertificate(m.cp), leafCertificate(m.api)
	now = now.Add(time.Hour + time.Minute)
	renewed, err := r.ensureTLS(t.Context(), gw, rolledDeployment(m))
	require.ErrorIs(t, err, errStop)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonSecretRefNotFound, certificateCondition(gw).Reason, "the stop is kept")
	assert.False(t, leafCertificate(renewed.cp).Equal(cpBefore), "the cp server certificate is renewed")
	assert.False(t, leafCertificate(renewed.api).Equal(apiBefore), "the API certificate is renewed")

	// The CAs rotate too.
	server := cpServerSigner(t, r)
	dRotate(gw)
	_, err = r.ensureTLS(t.Context(), gw, rolledDeployment(renewed))
	require.ErrorIs(t, err, errStop)
	assert.False(t, cpServerSigner(t, r).Equal(server))
}

// TestReconcile_ForeignDeploymentDoesntGate checks that a Deployment under
// the name of the GWAdmin it doesn't control drives none of its rotations:
// it runs no pod of the GWAdmin.
func TestReconcile_ForeignDeploymentDoesntGate(t *testing.T) {
	foreign := &appsv1.Deployment{
		Name: "gw", Namespace: testNamespace,
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foreign"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "foreign"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
			},
		},
	}
	e := newReconcileEnv(t, interceptor.Funcs{}, newGWAdmin(nil), foreign)
	_, gw, err := e.reconcile(t)
	require.NoError(t, err)
	requireReady(t, gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonResourceConflict)
	old := caCert(t, publishedCASecret(t, e.cl))

	gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
	require.NoError(t, e.cl.Update(t.Context(), gw))
	_, _, err = e.reconcile(t)
	require.NoError(t, err)
	// Nothing to wait for: the new cp client CA is published at once.
	assert.False(t, caCert(t, publishedCASecret(t, e.cl)).Equal(old))
}

// tGrant returns a KongReferenceGrant in the namespace of the GWAdmin gw/ns.
func tGrant(fromGroup, fromKind, fromNamespace string, toName *string) *configurationv1alpha1.KongReferenceGrant {
	g := &configurationv1alpha1.KongReferenceGrant{
		Namespace: testNamespace, Name: "data-planes",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group: configurationv1alpha1.Group(fromGroup), Kind: configurationv1alpha1.Kind(fromKind),
				Namespace: configurationv1alpha1.Namespace(fromNamespace),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: configurationv1alpha1.Group(operatorv1alpha1.SchemeGroupVersion.Group), Kind: operatorv1alpha1.GWAdminKind,
			}},
		},
	}
	if toName != nil {
		n := configurationv1alpha1.ObjectName(*toName)
		g.Spec.To[0].Name = &n
	}
	return g
}

func TestDataPlanes_Grants(t *testing.T) {
	dp := func(namespace string) *appsv1.Deployment {
		return dataPlaneDeploymentWith(namespace, "dp", "", "", true)
	}
	for _, tc := range []struct {
		name string
		objs []client.Object
		want []string
	}{
		{name: "same namespace counts", objs: []client.Object{dp(testNamespace)}, want: []string{"ns/dp"}},
		{name: "another namespace without a grant is ignored", objs: []client.Object{dp("other")}},
		{
			name: "another namespace with a grant counts",
			objs: []client.Object{dp("other"), tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "other", nil)},
			want: []string{"other/dp"},
		},
		{
			name: "a grant naming this GWAdmin counts",
			objs: []client.Object{dp("other"), tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "other", new("gw"))},
			want: []string{"other/dp"},
		},
		{
			name: "a grant to another GWAdmin is ignored",
			objs: []client.Object{dp("other"), tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "other", new("another"))},
		},
		{
			name: "a grant from another kind is ignored",
			objs: []client.Object{dp("other"), tGrant(operatorv1alpha1.SchemeGroupVersion.Group, "GatewayConfiguration", "other", nil)},
		},
		{
			name: "a grant for another namespace is ignored",
			objs: []client.Object{dp("other"), tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "elsewhere", nil)},
		},
		{
			name: "a grant outside the namespace of the GWAdmin is ignored",
			objs: []client.Object{dp("other"), func() client.Object {
				g := tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "other", nil)
				g.Namespace = "other"
				return g
			}()},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			r := newReconciler(t, &now, append(tc.objs, gw)...)
			dps, err := r.dataPlanes(t.Context(), gw)
			require.NoError(t, err)
			names := make([]string, 0, len(dps))
			for _, d := range dps {
				names = append(names, d.name)
			}
			sort.Strings(names)
			assert.Equal(t, tc.want, nilIfEmpty(names))
		})
	}
}

// TestDataPlanes_KegDataPlaneGrants checks a data plane Deployment counts in
// a namespace the KongReferenceGrants of the GWAdmin don't name when its
// KegDataPlane is allowed to reference a gateway of the GWAdmin in another
// namespace, and only then.
func TestDataPlanes_KegDataPlaneGrants(t *testing.T) {
	const team, tenant = "team", "tenant"
	kegDataPlane := func(gatewayNamespace string) *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			Name: "keg", Namespace: team, UID: "keg-uid",
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{ControlPlaneRef: eventgatewayv1alpha1.ControlPlaneRef{
				Type:                eventgatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef,
				OnPremNamespacedRef: &eventgatewayv1alpha1.OnPremNamespacedRef{Name: "gateway", Namespace: new(gatewayNamespace)},
			}},
		}
	}
	gateway := func(namespace, gwAdmin string) *eventgatewayv1alpha1.OnPremEventGateway {
		return &eventgatewayv1alpha1.OnPremEventGateway{
			Name: "gateway", Namespace: namespace,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: gwAdmin, Namespace: new(testNamespace)}},
		}
	}
	// deployment is the Deployment of the KegDataPlane keg of namespace team.
	deployment := func(ownerUID types.UID) *appsv1.Deployment {
		d := dataPlaneDeploymentWith(team, "keg", "", "", true)
		d.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: eventgatewayv1alpha1.SchemeGroupVersion.String(), Kind: "KegDataPlane",
			Name: "keg", UID: ownerUID, Controller: new(true),
		}}
		return d
	}
	// kegGrant lets the KegDataPlanes of namespace team reference the
	// OnPremEventGateways of namespace.
	kegGrant := func(namespace string) *configurationv1alpha1.KongReferenceGrant {
		return &configurationv1alpha1.KongReferenceGrant{
			Namespace: namespace, Name: "kegdataplanes",
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{Group: eventgatewayv1alpha1.GroupName, Kind: "KegDataPlane", Namespace: team}},
				To:   []configurationv1alpha1.ReferenceGrantTo{{Group: eventgatewayv1alpha1.GroupName, Kind: "OnPremEventGateway"}},
			},
		}
	}
	gatewayGrant := tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", tenant, nil)
	for _, tc := range []struct {
		name  string
		objs  []client.Object
		count bool
	}{
		{
			name:  "a granted KegDataPlane of a gateway of the namespace of the GWAdmin counts",
			objs:  []client.Object{deployment("keg-uid"), kegDataPlane(testNamespace), gateway(testNamespace, "gw"), kegGrant(testNamespace)},
			count: true,
		},
		{
			name:  "a granted KegDataPlane of a gateway of a namespace granted to the GWAdmin counts",
			objs:  []client.Object{deployment("keg-uid"), kegDataPlane(tenant), gateway(tenant, "gw"), kegGrant(tenant), gatewayGrant},
			count: true,
		},
		{
			name: "a KegDataPlane no KongReferenceGrant lets reference the gateway is ignored",
			objs: []client.Object{deployment("keg-uid"), kegDataPlane(testNamespace), gateway(testNamespace, "gw")},
		},
		{
			name: "a KegDataPlane of a gateway not allowed to use the GWAdmin is ignored",
			objs: []client.Object{deployment("keg-uid"), kegDataPlane(tenant), gateway(tenant, "gw"), kegGrant(tenant)},
		},
		{
			name: "a KegDataPlane of a gateway of another GWAdmin is ignored",
			objs: []client.Object{deployment("keg-uid"), kegDataPlane(testNamespace), gateway(testNamespace, "another"), kegGrant(testNamespace)},
		},
		{
			name: "a KegDataPlane of a gateway that doesn't exist is ignored",
			objs: []client.Object{deployment("keg-uid"), kegDataPlane(testNamespace), kegGrant(testNamespace)},
		},
		{
			name: "a Deployment of another incarnation of the KegDataPlane is ignored",
			objs: []client.Object{deployment("previous-uid"), kegDataPlane(testNamespace), gateway(testNamespace, "gw"), kegGrant(testNamespace)},
		},
		{
			name: "a Deployment without a KegDataPlane is ignored",
			objs: []client.Object{deployment("keg-uid"), gateway(testNamespace, "gw"), kegGrant(testNamespace)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			gw := newGWAdmin(nil)
			r := newReconciler(t, &now, append(tc.objs, gw)...)
			dps, err := r.dataPlanes(t.Context(), gw)
			require.NoError(t, err)
			if tc.count {
				require.Len(t, dps, 1)
				assert.Equal(t, team+"/keg", dps[0].name)
				return
			}
			assert.Empty(t, dps)
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// TestEnsureTLS_SkipDataPlanesAnnotation checks that rotations stop waiting
// for the data planes while the annotation is set, with a warning.
func TestEnsureTLS_SkipDataPlanesAnnotation(t *testing.T) {
	now := time.Now()
	gw := newGWAdmin(nil)
	r := newReconciler(t, &now, gw)
	m := dSteady(t, r, gw)
	rollDataPlane(t, r, dDataPlaneNamespace, dDataPlaneName, m)
	old := cpServerSigner(t, r)

	dRotate(gw)
	m = ensure(t, r, gw, rolledDeployment(m))
	requireSignedBy(t, old, m.cp)
	assert.Contains(t, certificateCondition(gw).Message, "data plane Deployment dp-ns/dp")

	gw.Annotations[consts.GWAdminRotationSkipDataPlanesAnnotation] = "true"
	m = ensure(t, r, gw, rolledDeployment(m))
	requireSignedBy(t, cpServerSigner(t, r), m.cp)
	assert.Contains(t, certificateCondition(gw).Message, "rotations don't wait for the data planes")
}

func TestEnqueueForGrant(t *testing.T) {
	now := time.Now()
	a, b := newGWAdmin(nil), newGWAdmin(nil)
	b.Name, b.UID = "gw-b", "gw-b-uid"
	elsewhere := newGWAdmin(nil)
	elsewhere.Namespace = "other"
	r := newReconciler(t, &now, a, b, elsewhere)

	reqs := r.enqueueForGrant(t.Context(), tGrant(eventgatewayv1alpha1.GroupName, "OnPremEventGateway", "x", nil))
	names := make([]string, 0, len(reqs))
	for _, req := range reqs {
		require.Equal(t, testNamespace, req.Namespace)
		names = append(names, req.Name)
	}
	sort.Strings(names)
	assert.Equal(t, []string{"gw", "gw-b"}, names)
}

// TestEnsureTLS_ManualCAClockSkew checks that a Manual CA whose NotBefore is
// at most 5 minutes ahead is accepted, and later ones rejected until then.
func TestEnsureTLS_ManualCAClockSkew(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, tc := range []struct {
		name      string
		notBefore time.Time
		accepted  bool
	}{
		{name: "3 minutes ahead", notBefore: now.Add(3 * time.Minute), accepted: true},
		{name: "10 minutes ahead", notBefore: now.Add(10 * time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := now
			ca := selfSignedCA(t, now, nil, func(c *x509.Certificate) { c.NotBefore = tc.notBefore })
			gw := newGWAdmin(&operatorv1alpha1.GWAdminTLS{ClientCA: manualCA(userCASecretName)})
			r := newReconciler(t, &now, gw, ca)
			_, err := r.ensureTLS(t.Context(), gw, nil)
			if tc.accepted {
				require.NoError(t, err)
				assert.True(t, publishedClientCA(t, r).Equal(caCert(t, ca)))
				return
			}
			require.ErrorIs(t, err, errStop)
			assert.Equal(t, tc.notBefore.Add(-pki.ClockSkew).Sub(now), stopRequeue(err))
			assert.Equal(t, operatorv1alpha1.GWAdminReasonInvalidSecret, certificateCondition(gw).Reason)
		})
	}
}
