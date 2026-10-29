package gwadmin

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	testNamespace         = "ns"
	testOperatorNamespace = "kong-system"
)

func newGWAdmin(tlsSpec *operatorv1alpha1.GWAdminTLS) *operatorv1alpha1.GWAdmin {
	return &operatorv1alpha1.GWAdmin{
		Name: "gw", Namespace: testNamespace, UID: "gw-uid", Generation: 1,
		Spec: operatorv1alpha1.GWAdminSpec{TLS: tlsSpec},
	}
}

func newReconciler(t *testing.T, now *time.Time, objs ...client.Object) *Reconciler {
	t.Helper()
	return reconcilerWith(fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).Build(), now)
}

// userCASecretName is the name of the Secret userCASecret returns.
const userCASecretName = "user-ca"

// userCASecret returns a cert-manager shaped CA Secret named userCASecretName,
// holding a certificate authority with commonName, valid for a year from now.
func userCASecret(t *testing.T, commonName string) *corev1.Secret {
	t.Helper()
	return userCASecretValidAt(t, commonName, time.Now(), 365*24*time.Hour)
}

// userCASecretValidAt is userCASecret for a certificate authority generated
// at now (backdated by 5 minutes) and valid for ttl.
func userCASecretValidAt(t *testing.T, commonName string, now time.Time, ttl time.Duration) *corev1.Secret {
	t.Helper()
	certPEM, keyPEM, err := generateCA(commonName, now, ttl)
	require.NoError(t, err)
	return &corev1.Secret{
		Name: userCASecretName, Namespace: testNamespace, Labels: map[string]string{"konghq.com/secret": "true"},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM, "ca.crt": certPEM},
	}
}

// rotateManualCA replaces the certificate authority in the user's CA Secret,
// as cert-manager renewing it in place would, and returns the new one.
func rotateManualCA(t *testing.T, r *Reconciler, commonName string) *x509.Certificate {
	t.Helper()
	next := userCASecret(t, commonName)
	live := getSecret(t, r, userCASecretName)
	live.Data = next.Data
	require.NoError(t, r.Update(t.Context(), live))
	return caCert(t, next)
}

// rolledDeployment returns a Deployment whose pods all run the material m:
// both its checksum and its trust checksum.
func rolledDeployment(m *tlsMaterial) *appsv1.Deployment {
	return rolledWith(m.checksum(), m.trustChecksum())
}

// rolledWith returns a Deployment whose pods all run the given checksum and
// trust checksum.
func rolledWith(checksum, trust string) *appsv1.Deployment {
	return &appsv1.Deployment{
		Name: "gw", Namespace: testNamespace, Generation: 1,
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "gw"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					consts.GWAdminTLSChecksumAnnotation:   checksum,
					consts.GWAdminTrustChecksumAnnotation: trust,
				},
			}},
		},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
}

// rollingDeployment returns a Deployment whose spec carries the material m
// while no pod runs it yet: a rollout that never progresses.
func rollingDeployment(m *tlsMaterial) *appsv1.Deployment {
	d := rolledDeployment(m)
	d.Status.UpdatedReplicas = 0
	d.Status.AvailableReplicas = 0
	return d
}

func caCert(t *testing.T, s *corev1.Secret) *x509.Certificate {
	t.Helper()
	certs := pki.ParseCertificates(s.Data[corev1.TLSCertKey])
	require.NotEmpty(t, certs)
	return certs[0]
}

// clientBundleCerts returns the certificates the cp listener verifies the
// data plane certificates with.
func clientBundleCerts(m *tlsMaterial) []*x509.Certificate { return bundleCerts(m.clientBundle) }

// serverBundleCerts returns the certificates the data planes verify the cp
// listener with.
func serverBundleCerts(m *tlsMaterial) []*x509.Certificate { return bundleCerts(m.serverBundle) }

// apiBundleCerts returns the certificates of the trust bundle of the API listener.
func apiBundleCerts(m *tlsMaterial) []*x509.Certificate { return bundleCerts(m.apiBundle) }

// apiSigner returns the API CA.
func apiSigner(t *testing.T, r *Reconciler) *x509.Certificate {
	t.Helper()
	return caCert(t, getPrivateSecret(t, r, "gwadmin-gw-uid-api-ca"))
}

// cpServerSigner returns the cp server CA.
func cpServerSigner(t *testing.T, r *Reconciler) *x509.Certificate {
	t.Helper()
	return caCert(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-server-ca"))
}

// publishedClientCA returns the published cp client CA.
func publishedClientCA(t *testing.T, r *Reconciler) *x509.Certificate {
	t.Helper()
	return caCert(t, getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca"))
}

// requireNotSignedBy fails when any of the certificates is signed by ca.
func requireNotSignedBy(t *testing.T, ca *x509.Certificate, leaves ...*corev1.Secret) {
	t.Helper()
	for _, l := range leaves {
		c := leafCertificate(l)
		require.NotNil(t, c, l.Name)
		require.Error(t, c.CheckSignatureFrom(ca), l.Name)
	}
}

func requireSignedBy(t *testing.T, ca *x509.Certificate, leaves ...*corev1.Secret) {
	t.Helper()
	for _, l := range leaves {
		c := leafCertificate(l)
		require.NotNil(t, c, l.Name)
		require.NoError(t, c.CheckSignatureFrom(ca), l.Name)
	}
}

func certificateCondition(gw *operatorv1alpha1.GWAdmin) *metav1.Condition {
	for i := range gw.Status.Conditions {
		if gw.Status.Conditions[i].Type == operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned {
			return &gw.Status.Conditions[i]
		}
	}
	return nil
}

func manualCA(secretName string) *operatorv1alpha1.GWAdminCertificateSecret {
	return &operatorv1alpha1.GWAdminCertificateSecret{
		Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
		SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: secretName},
	}
}

// serverUsages are the usages of a server certificate the user brings.
var serverUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}

// userCPSecretName is the name of the Secret userCPSecret returns.
const userCPSecretName = "user-cp"

// manualCP references the user's cp certificate Secret userCPSecretName.
func manualCP() *operatorv1alpha1.GWAdminServerCertificate {
	return &operatorv1alpha1.GWAdminServerCertificate{
		Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
		SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: userCPSecretName},
	}
}

// userCPSecret returns the user's cp server certificate Secret userCPSecretName,
// issued by a CA of its own at issuedAt (the zero time for the current time).
// With withChain, tls.crt carries that CA after the certificate; otherwise
// ca.crt holds it, as cert-manager does.
func userCPSecret(t *testing.T, issuedAt time.Time, withChain bool) (*corev1.Secret, *x509.Certificate) {
	t.Helper()
	ca := userCASecret(t, "public")
	return userCPSecretFrom(t, ca, issuedAt, 24*time.Hour, withChain), caCert(t, ca)
}

// userCPSecretFrom is userCPSecret for a certificate issued by ca, valid for ttl.
func userCPSecretFrom(t *testing.T, ca *corev1.Secret, issuedAt time.Time, ttl time.Duration, withChain bool, dnsNames ...string) *corev1.Secret {
	t.Helper()
	if len(dnsNames) == 0 {
		dnsNames = []string{"cp.example.com"}
	}
	certPEM, keyPEM, err := issueCertificate(ca, certRequest{
		commonName: dnsNames[0], dnsNames: dnsNames,
		usages: serverUsages, ttl: ttl, now: issuedAt,
	})
	require.NoError(t, err)
	data := map[string][]byte{corev1.TLSPrivateKeyKey: keyPEM}
	if withChain {
		data[corev1.TLSCertKey] = append(certPEM, ca.Data[corev1.TLSCertKey]...)
	} else {
		data[corev1.TLSCertKey] = certPEM
		data[consts.CACRT] = ca.Data[corev1.TLSCertKey]
	}
	return &corev1.Secret{
		Name: userCPSecretName, Namespace: testNamespace, Labels: map[string]string{"konghq.com/secret": "true"},
		Type: corev1.SecretTypeTLS,
		Data: data,
	}
}

// ensure runs one ensureTLS pass, which must succeed.
func ensure(t *testing.T, r *Reconciler, gw *operatorv1alpha1.GWAdmin, d *appsv1.Deployment) *tlsMaterial {
	t.Helper()
	m, err := r.ensureTLS(t.Context(), gw, d)
	require.NoError(t, err)
	return m
}

// getSecret returns the named Secret of the GWAdmin namespace.
func getSecret(t *testing.T, r *Reconciler, name string) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: name}, s))
	return s
}

// getPrivateSecret returns the named Secret of the operator namespace.
func getPrivateSecret(t *testing.T, r *Reconciler, name string) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Namespace: testOperatorNamespace, Name: name}, s))
	return s
}

func leaves(m *tlsMaterial) []*corev1.Secret { return []*corev1.Secret{m.api, m.cp, m.client} }

// requireSameLeaves fails when any certificate of b differs from a's.
func requireSameLeaves(t *testing.T, a, b *tlsMaterial) {
	t.Helper()
	for i, l := range leaves(a) {
		require.True(t, leafCertificate(l).Equal(leafCertificate(leaves(b)[i])), "%s was reissued", l.Name)
	}
}

// verifyWith verifies the certificate of secret, for usage, against the
// certificate authorities of bundle.
func verifyWith(secret *corev1.Secret, bundle []*x509.Certificate, usage x509.ExtKeyUsage) error {
	roots := x509.NewCertPool()
	for _, c := range bundle {
		roots.AddCert(c)
	}
	intermediates := x509.NewCertPool()
	for _, c := range pki.ParseCertificates(secret.Data[corev1.TLSCertKey])[1:] {
		intermediates.AddCert(c)
	}
	_, err := leafCertificate(secret).Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{usage},
	})
	return err
}

// drainEvents returns the events recorded so far.
func drainEvents(rec *events.FakeRecorder) []string {
	var got []string
	for {
		select {
		case e := <-rec.Events:
			got = append(got, e)
		default:
			return got
		}
	}
}

func hasEvent(evts []string, prefix string) bool {
	for _, e := range evts {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// dataPlaneDeploymentWith returns a data plane Deployment of the GWAdmin
// gw/ns whose Pod template records the given cp server bundle checksum and
// client CA fingerprint, and whose pods all run it when rolled.
func dataPlaneDeploymentWith(dpNamespace, name, serverBundleChecksum, clientCA string, rolled bool) *appsv1.Deployment {
	d := &appsv1.Deployment{
		Name: name, Namespace: dpNamespace, Generation: 1,
		Labels: map[string]string{
			consts.GWAdminDataPlaneNamespaceLabel: testNamespace,
			consts.GWAdminDataPlaneNameLabel:      "gw",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				consts.GWAdminDataPlaneServerBundleAnnotation: serverBundleChecksum,
				consts.GWAdminDataPlaneClientCAAnnotation:     clientCA,
			}}},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, TerminatingReplicas: new(int32(0)),
		},
	}
	if !rolled {
		d.Status.UpdatedReplicas = 0
	}
	return d
}

// rollDataPlane makes the data plane Deployment run the cp server bundle and
// the published cp client CA of m, as the controller managing it would.
func rollDataPlane(t *testing.T, r *Reconciler, dpNamespace, name string, m *tlsMaterial) {
	t.Helper()
	clientCA := getPrivateSecret(t, r, "gwadmin-gw-uid-cp-client-ca")
	setDataPlane(t, r, dpNamespace, name, map[string]string{
		consts.GWAdminDataPlaneServerBundleAnnotation: gwadminref.DataPlaneServerBundleChecksum(m.serverBundle),
		consts.GWAdminDataPlaneClientCAAnnotation:     gwadminref.DataPlaneClientCAFingerprint(clientCA.Data[corev1.TLSCertKey]),
	}, true)
}

// apiServerPatch patches as the API server does: a JSON patch that doesn't
// apply fails with an Invalid error, which the fake client doesn't wrap.
func apiServerPatch(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	err := c.Patch(ctx, obj, patch, opts...)
	var status apierrors.APIStatus
	if err != nil && !errors.As(err, &status) && patch.Type() == types.JSONPatchType {
		return apierrors.NewGenericServerResponse(http.StatusUnprocessableEntity, "", schema.GroupResource{}, "", err.Error(), 0, false)
	}
	return err
}

// reconcilerWith returns a Reconciler over cl, at the time now points to.
func reconcilerWith(cl client.Client, now *time.Time) *Reconciler {
	return &Reconciler{
		Client:              cl,
		ClusterDomain:       "cluster.local",
		SecretLabelSelector: "konghq.com/secret",
		OperatorNamespace:   testOperatorNamespace,
		Now:                 func() time.Time { return *now },
	}
}

// hidingClient returns a client over base whose Get misses the Secret named
// hidden, as the cache does before it caught up, or for a Secret without
// its selector label. Its JSON patches fail as the API server's do.
func hidingClient(base client.WithWatch, hidden string) client.WithWatch {
	return interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Secret); ok && key.Name == hidden {
				return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Patch: apiServerPatch,
	})
}

// replaceUserCP replaces the data of the user's cp certificate Secret.
func replaceUserCP(t *testing.T, r *Reconciler, next *corev1.Secret) {
	t.Helper()
	live := getSecret(t, r, userCPSecretName)
	live.Data = next.Data
	require.NoError(t, r.Update(t.Context(), live))
}

func rsaKey(t *testing.T, bits int) crypto.Signer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	return k
}

// selfSignedCA returns a CA Secret named userCASecretName, visible to the
// operator, holding a certificate authority for key (a P-256 one when nil)
// whose template mutate changes, when not nil.
func selfSignedCA(t *testing.T, now time.Time, key crypto.Signer, mutate func(*x509.Certificate)) *corev1.Secret {
	t.Helper()
	if key == nil {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "user"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if mutate != nil {
		mutate(template)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return &corev1.Secret{
		Name: userCASecretName, Namespace: testNamespace, Type: corev1.SecretTypeTLS,
		Labels: map[string]string{"konghq.com/secret": "true"},
		Data: map[string][]byte{
			corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		},
	}
}

// grantDataPlanes creates, unless it exists, a KongReferenceGrant in the
// namespace of the GWAdmin gw/ns allowing its data planes in namespace, as
// its owner would for the OnPremEventGateways there.
func grantDataPlanes(t *testing.T, r *Reconciler, namespace string) {
	t.Helper()
	if namespace == testNamespace {
		return
	}
	grant := &configurationv1alpha1.KongReferenceGrant{
		Namespace: testNamespace, Name: "data-planes-" + namespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group: eventgatewayv1alpha1.GroupName, Kind: "OnPremEventGateway",
				Namespace: configurationv1alpha1.Namespace(namespace),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: configurationv1alpha1.Group(operatorv1alpha1.SchemeGroupVersion.Group), Kind: operatorv1alpha1.GWAdminKind,
			}},
		},
	}
	if err := r.Create(t.Context(), grant); err != nil {
		require.True(t, apierrors.IsAlreadyExists(err), err)
	}
}

// setDataPlane creates or updates the data plane Deployment name in
// namespace dpNamespace of the GWAdmin gw/ns, setting the given Pod template
// annotations, and makes its rollout complete when rolled, or never complete
// otherwise, as the Deployment controller would.
func setDataPlane(t *testing.T, r *Reconciler, dpNamespace, name string, annotations map[string]string, rolled bool) {
	t.Helper()
	grantDataPlanes(t, r, dpNamespace)
	live := &appsv1.Deployment{}
	if err := r.Get(t.Context(), types.NamespacedName{Namespace: dpNamespace, Name: name}, live); err != nil {
		require.True(t, apierrors.IsNotFound(err), err)
		d := dataPlaneDeploymentWith(dpNamespace, name, "", "", rolled)
		d.Spec.Template.Annotations = annotations
		require.NoError(t, r.Create(t.Context(), d))
		return
	}
	maps.Copy(live.Spec.Template.Annotations, annotations)
	require.NoError(t, r.Update(t.Context(), live))
	// The status is a subresource: it is written on its own.
	live.Status = dataPlaneDeploymentWith(dpNamespace, name, "", "", rolled).Status
	live.Status.ObservedGeneration = live.Generation
	require.NoError(t, r.Status().Update(t.Context(), live))
}
