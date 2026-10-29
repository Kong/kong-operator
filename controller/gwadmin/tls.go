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

// TLS model of a GWAdmin
//
// gw-admin has two listeners and three trust domains, each a certificate
// authority, the trust bundle its verifiers use and the certificates issued
// from it:
//
//   - API: Kong Operator and the API listener authenticate each other. The
//     CA is Kong Operator's; its key and the operator client certificate
//     live in the operator namespace. The bundle is mounted into the pods
//     and read by Kong Operator.
//   - cp server: the data planes verify the cp listener. The CA is Kong
//     Operator's; its key lives in the operator namespace. The bundle is
//     published for the data planes; the pods don't mount it. A Manual cp
//     certificate brings its own issuers into that bundle.
//   - cp client: the cp listener verifies the data plane certificates. The
//     CA is Automatic or the user's. Once the pods trust it, it is copied,
//     with its key, into the operator namespace (gwadminref.CPClientCASecretName), where
//     the controller managing the data planes issues their certificates from
//     it. The bundle is mounted into the pods.
//
// Invariant: a certificate goes live only once every peer verifying it
// trusts its issuer. Renewing a certificate under the same authority is
// always safe. The peers that matter are:
//
//   - the gw-admin pods, for the operator client certificate and the data
//     plane certificates: they trust what the Deployment rolled out, so a new
//     authority is added to a mounted bundle first, and used once the
//     rollout completed. gw-admin doesn't reload certificates: every change
//     of the mounted material rolls the pods.
//   - the data planes, for the cp server certificate, and for the previous
//     cp client CA they may still hold certificates from. They are the
//     Deployments, in any namespace, labelled with the GWAdmin, whose Pod
//     template records what they mount: a new issuer is added to the
//     published bundle first, and used once every data plane rolled out
//     with that bundle; the previous cp client CA is dropped once every data
//     plane rolled out with a certificate from the published one.
//   - Kong Operator, for the API server certificate: it reads the API bundle
//     on every connection.
//
// No phase is recorded anywhere: each reconcile derives it from the bundles,
// from which authority signed each certificate, and from what the gw-admin
// and data plane Deployments run. The gw-admin Pod template records two
// checksums: one of the mounted trust bundles, to know the pods trust a new
// authority, and one of all the mounted material, to know they no longer
// serve a previous certificate.
//
// A certificate that expired is broken for every peer, so it is reissued
// without waiting: waiting would only prolong the outage.

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// minRSAKeyBits is the smallest RSA key accepted in a Manual certificate
// authority.
const minRSAKeyBits = 2048

// errStop is returned when the TLS material can't be provisioned for a
// reason the user has to fix. The reason is reported in the
// CertificateProvisioned condition, and a watch event re-triggers the
// reconcile once it is fixed.
var errStop = errors.New("TLS material can't be provisioned")

// errCacheStale is returned when a write shows that the cache doesn't hold
// the current version of a Secret yet. Nothing is wrong: the reconcile is
// retried shortly, without reporting anything.
var errCacheStale = errors.New("the cache doesn't hold the current Secret yet")

// stopError is an errStop that also needs a reconcile after a delay, as no
// watch event fires when the reason goes away, e.g. a certificate that only
// becomes valid later.
type stopError struct {
	after time.Duration
}

func (e *stopError) Error() string { return errStop.Error() }

// Is makes a stopError match errStop.
func (e *stopError) Is(target error) bool { return target == errStop }

// stopRequeue returns the delay until a GWAdmin stopped by err is reconciled
// again.
func stopRequeue(err error) time.Duration {
	if se, ok := errors.AsType[*stopError](err); ok && se.after > 0 {
		return min(se.after, pki.MaxRenewalRequeue)
	}
	return pki.MaxRenewalRequeue
}

const (
	// certificateSourceAnnotation marks a Secret holding a copy of the
	// user's Manual certificate authority or cp certificate.
	certificateSourceAnnotation = "konghq.com/gw-admin-certificate-source"
	certificateSourceManual     = "manual"

	// caPublishedAtAnnotation records, on the published cp client CA, when
	// it was published. Its gwadminref.PreviousCAKey holds the certificate authorities
	// published before it that the data planes may still use.
	caPublishedAtAnnotation = "konghq.com/gw-admin-ca-published-at"

	// servedCAKey holds, on the cp server CA Secret, the issuers of the
	// certificate the cp listener serves.
	servedCAKey = "served-ca.crt"

	// rotationTokenAnnotation records, on a certificate authority Kong
	// Operator created, the value of the GWAdminRotateCAsAnnotation of the
	// GWAdmin it was created under: a different value rotates it.
	rotationTokenAnnotation = "konghq.com/gw-admin-rotation-token" //nolint:gosec
)

// managedAnnotations are the annotations of its Secrets Kong Operator sets
// or removes, leaving the others alone.
var managedAnnotations = []string{certificateSourceAnnotation, caPublishedAtAnnotation, rotationTokenAnnotation}

// The Secrets of a GWAdmin in its namespace are its name followed by a
// suffix. No suffix ends with another one, so that the Secrets of two
// GWAdmins never collide, e.g. those of foo and foo-api.

func apiCABundleSecretName(gw *operatorv1alpha1.GWAdmin) string { return gw.Name + "-api-ca-bundle" }
func apiTLSSecretName(gw *operatorv1alpha1.GWAdmin) string      { return gw.Name + "-api-tls" }
func cpTLSSecretName(gw *operatorv1alpha1.GWAdmin) string       { return gw.Name + "-cp-tls" }
func cpServerCABundleSecretName(gw *operatorv1alpha1.GWAdmin) string {
	return gw.Name + "-cp-server-ca-bundle"
}
func cpClientCABundleSecretName(gw *operatorv1alpha1.GWAdmin) string {
	return gw.Name + "-cp-client-ca-bundle"
}

// ownedSecretNames returns the names of the Secrets the GWAdmin owns in its
// namespace.
func ownedSecretNames(gw *operatorv1alpha1.GWAdmin) []string {
	return []string{
		apiCABundleSecretName(gw), apiTLSSecretName(gw), cpTLSSecretName(gw), cpServerCABundleSecretName(gw),
		cpClientCABundleSecretName(gw),
	}
}

// The Secrets only Kong Operator uses live in the operator namespace, named
// after the UID of the GWAdmin.

// APICASecretName returns the name of the Secret, in the operator namespace,
// holding the API CA of the GWAdmin.
func APICASecretName(gw *operatorv1alpha1.GWAdmin) string {
	return gwadminref.OperatorSecretPrefix(gw) + "-api-ca"
}

// CPServerCASecretName returns the name of the Secret, in the operator
// namespace, holding the cp server CA of the GWAdmin.
func CPServerCASecretName(gw *operatorv1alpha1.GWAdmin) string {
	return gwadminref.OperatorSecretPrefix(gw) + "-cp-server-ca"
}

func cpClientCANextSecretName(gw *operatorv1alpha1.GWAdmin) string {
	return gwadminref.OperatorSecretPrefix(gw) + "-cp-client-ca-next"
}

// privateSecretNames returns the names of the Secrets the GWAdmin keeps in
// the operator namespace.
func privateSecretNames(gw *operatorv1alpha1.GWAdmin) []string {
	return []string{
		APICASecretName(gw), CPServerCASecretName(gw), gwadminref.OperatorClientTLSSecretName(gw),
		gwadminref.CPClientCASecretName(gw), cpClientCANextSecretName(gw),
	}
}

// secretRef locates a Secret of a GWAdmin: in its namespace, controlled by
// it, or, when private, in the operator namespace, labelled with its UID.
type secretRef struct {
	namespace, name string
	private         bool
}

func (r *Reconciler) local(gw *operatorv1alpha1.GWAdmin, name string) secretRef {
	return secretRef{namespace: gw.Namespace, name: name}
}

func (r *Reconciler) private(name string) secretRef {
	return secretRef{namespace: r.OperatorNamespace, name: name, private: true}
}

func (s secretRef) key() types.NamespacedName {
	return types.NamespacedName{Namespace: s.namespace, Name: s.name}
}

// ownsSecret reports whether secret, found at ref, belongs to the GWAdmin.
func ownsSecret(gw *operatorv1alpha1.GWAdmin, ref secretRef, secret *corev1.Secret) bool {
	if ref.private {
		return secret.Labels[consts.GWAdminUIDLabel] == string(gw.UID)
	}
	return metav1.IsControlledBy(secret, gw)
}

// isManualCA reports whether the cp client CA is the user's. The CRD
// requires secretRef with Manual provisioning; the nil check keeps an object
// stored before that validation from crashing the operator.
func isManualCA(gw *operatorv1alpha1.GWAdmin) bool {
	return gw.Spec.TLS != nil && gw.Spec.TLS.ClientCA != nil &&
		gw.Spec.TLS.ClientCA.Provisioning == operatorv1alpha1.GWAdminCertificateProvisioningManual &&
		gw.Spec.TLS.ClientCA.SecretRef != nil
}

// isManualCP reports whether the cp server certificate is the user's. See
// isManualCA for the nil check.
func isManualCP(gw *operatorv1alpha1.GWAdmin) bool {
	return gw.Spec.TLS != nil && gw.Spec.TLS.CP != nil &&
		gw.Spec.TLS.CP.Provisioning == operatorv1alpha1.GWAdminCertificateProvisioningManual &&
		gw.Spec.TLS.CP.SecretRef != nil
}

// rotationToken returns the value of the GWAdminRotateCAsAnnotation of the
// GWAdmin, recorded on the certificate authorities Kong Operator creates.
func rotationToken(gw *operatorv1alpha1.GWAdmin) string {
	return gw.Annotations[consts.GWAdminRotateCAsAnnotation]
}

// rotationRequested reports whether the GWAdmin asks to rotate the
// certificate authority held in secret: its GWAdminRotateCAsAnnotation is set
// to a value the certificate authority wasn't created under. Removing the
// annotation requests nothing, e.g. a GitOps tool pruning it.
func rotationRequested(gw *operatorv1alpha1.GWAdmin, secret *corev1.Secret) bool {
	v, ok := gw.Annotations[consts.GWAdminRotateCAsAnnotation]
	return ok && secret.Annotations[rotationTokenAnnotation] != v
}

// tlsMaterial is the TLS material of a GWAdmin, as provisioned by ensureTLS.
type tlsMaterial struct {
	// Mounted into the pods.
	apiBundle    *corev1.Secret
	api          *corev1.Secret
	cp           *corev1.Secret
	clientBundle *corev1.Secret
	// Not mounted.
	serverBundle *corev1.Secret
	client       *corev1.Secret

	// rotating is true while certificates move to a new certificate authority.
	rotating bool
	// progress tells how far the rotations in progress are.
	progress []string
	// blocked lists the certificates past their renewal time that can't
	// move to the new certificate authority before the pods roll out.
	blocked []string
	// warnings are reported in the CertificateProvisioned condition without
	// stopping anything.
	warnings []string
	// expiring are the warnings about certificates the user provides that
	// are about to expire, also recorded as events.
	expiring []string
	// due lists the moments something must be looked at again.
	due []time.Time
	// requeueAfter is the delay until the next reconcile.
	requeueAfter time.Duration
}

// warnExpiring reports a certificate the user provides that is about to
// expire.
func (m *tlsMaterial) warnExpiring(msg string) {
	m.warnings = append(m.warnings, msg)
	m.expiring = append(m.expiring, msg)
}

// mountable reports whether every Secret mounted into the pods exists, so
// that the Deployment can be applied.
func (m *tlsMaterial) mountable() bool {
	return m != nil && m.apiBundle != nil && m.api != nil && m.cp != nil && m.clientBundle != nil
}

// checksum returns the checksum of the TLS material mounted into the pods.
func (m *tlsMaterial) checksum() string {
	return tlsChecksum(m.apiBundle, m.clientBundle, m.api, m.cp)
}

// trustChecksum returns the checksum of the trust bundles mounted into the pods.
func (m *tlsMaterial) trustChecksum() string {
	return tlsChecksum(m.apiBundle, m.clientBundle)
}

// tlsChecksum returns a checksum of the given Secrets mounted into the
// gw-admin pods, over the keys the pods mount.
func tlsChecksum(mounted ...*corev1.Secret) string {
	h := sha256.New()
	for _, s := range mounted {
		if s == nil {
			h.Write([]byte{0})
			continue
		}
		for _, k := range []string{consts.CACRT, corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
			h.Write(s.Data[k])
			h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// rolloutState tells how far the gw-admin Deployment rolled out.
type rolloutState struct {
	// complete is true when every pod runs the current Pod template and no
	// old pod is terminating.
	complete bool
	// terminating is true when the rollout only waits for old pods to go.
	terminating bool
	// annotations are those of the Pod template the pods run once complete.
	annotations map[string]string
	// description tells why the rollout isn't complete, for the conditions.
	description string
}

// runs reports whether every pod runs the Pod template annotation key set to
// value.
func (s rolloutState) runs(key, value string) bool {
	return s.complete && s.annotations[key] == value
}

// rolloutOf returns how far the Deployment rolled out. Old pods are still
// around while a rolling update is in progress, and they may not trust a new
// certificate authority yet. A terminating pod is out of the Deployment
// status, but a Service publishing not ready addresses still sends it
// traffic, so it counts as an old pod, as far as status.terminatingReplicas
// reports it: without the DeploymentReplicaSetTerminatingReplicas feature, a
// pod being killed is not waited for.
func rolloutOf(d *appsv1.Deployment) rolloutState {
	if d == nil {
		return rolloutState{description: "the Deployment doesn't exist yet"}
	}
	state := rolloutState{annotations: d.Spec.Template.Annotations}
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	st := d.Status
	switch {
	case st.ObservedGeneration < d.Generation || st.UpdatedReplicas != replicas || st.Replicas != replicas:
		state.description = describeRollout(d, replicas)
	case st.TerminatingReplicas != nil && *st.TerminatingReplicas > 0:
		state.terminating = true
		state.description = "waiting for the old pods to terminate"
	default:
		state.complete = true
	}
	return state
}

// describeRollout tells why the Deployment didn't roll out yet.
func describeRollout(d *appsv1.Deployment, replicas int32) string {
	if d.Spec.Paused {
		return "the Deployment is paused"
	}
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse {
			return fmt.Sprintf("the Deployment rollout is not progressing (%s: %s)", c.Reason, c.Message)
		}
	}
	return fmt.Sprintf("waiting for the Deployment to roll out (%d of %d pods updated, %d available)",
		d.Status.UpdatedReplicas, replicas, d.Status.AvailableReplicas)
}

// pass is what a reconcile knows about the pods, kept up to date as it
// changes the mounted material.
type pass struct {
	now time.Time
	// running is true when the gw-admin Deployment exists: its pods may
	// verify what changes.
	running bool
	// trusted is true when the pods trust the mounted bundles as written.
	trusted bool
	// rolled is true when the pods run the mounted material as written.
	rolled bool
	// rolloutDescription tells why the gw-admin rollout isn't complete.
	rolloutDescription string
}

func (p *pass) bundleChanged() { p.trusted, p.rolled = false, false }
func (p *pass) leafChanged()   { p.rolled = false }

// leafSpec describes a certificate Kong Operator issues for a GWAdmin.
type leafSpec struct {
	commonName string
	dnsNames   []string
	usages     []x509.ExtKeyUsage
	// mounted is true for the certificates mounted into the pods: the pods
	// can't start without them.
	mounted bool
}

// serverLeafSpec describes the server certificate of a Service. The DNS
// names identify it; its common name is the Service name, which always fits
// in the 64 characters RFC 5280 allows.
func serverLeafSpec(serviceName, namespace, clusterDomain string) leafSpec {
	return leafSpec{
		commonName: serviceName,
		dnsNames:   serviceDNSNames(serviceName, namespace, clusterDomain),
		usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		mounted:    true,
	}
}

func operatorClientLeafSpec() leafSpec {
	return leafSpec{
		commonName: consts.GWAdminOperatorClientCommonName,
		usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
}

// serviceDNSNames returns the in-cluster DNS names of a Service.
func serviceDNSNames(serviceName, namespace, clusterDomain string) []string {
	names := []string{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
	}
	if clusterDomain != "" {
		names = append(names, serviceName+"."+namespace+".svc."+clusterDomain)
	}
	return names
}

// renewalTime returns when a certificate is due for renewal: once two thirds
// of its validity have elapsed.
func renewalTime(cert *x509.Certificate) time.Time {
	return pki.RenewalTime(cert.NotBefore, cert.NotAfter)
}

// warningTime returns when a certificate the user provides is about to
// expire: once less than a sixth of its validity is left, half of the
// renewal window cert-manager leaves by default.
func warningTime(cert *x509.Certificate) time.Time {
	return cert.NotAfter.Add(-cert.NotAfter.Sub(cert.NotBefore) / 6)
}

// leafIssue tells whether a certificate must be issued, and whether issuing
// it must wait for the pods to trust the signer.
type leafIssue struct {
	needed bool
	// gated is true when the pods may not trust the signer of the new
	// certificate until they rolled out.
	gated bool
}

// leafNeedsIssue decides whether the certificate held in secret must be
// issued again from signer to match spec. running is true when the gw-admin
// Deployment exists, so that its pods may not trust a new signer yet. ttl is
// the validity a certificate issued again gets.
func leafNeedsIssue(secret *corev1.Secret, spec leafSpec, signer *x509.Certificate, now time.Time, running bool, ttl time.Duration) leafIssue {
	leaf := leafCertificate(secret)
	switch {
	case leaf == nil:
		// Nothing works without it: it is issued right away. A certificate
		// the pods may not trust yet still beats none, and waiting could
		// never end, e.g. new pods only turn Ready once Kong Operator
		// provisions them with the operator client certificate.
		return leafIssue{needed: true}
	case leaf.CheckSignatureFrom(signer) != nil:
		// Without pods, there is nobody to wait for.
		return leafIssue{needed: true, gated: running}
	case leaf.Subject.CommonName != spec.commonName || !sameNames(leaf.DNSNames, spec.dnsNames):
		return leafIssue{needed: true}
	case !now.Before(renewalTime(leaf)) && renewalExtends(leaf, signer, now, ttl):
		return leafIssue{needed: true}
	default:
		return leafIssue{}
	}
}

// renewalExtends reports whether issuing the certificate again from signer
// would at least double its remaining validity. A certificate capped at the
// expiry of its certificate authority can't be extended: renewing it would
// only roll the pods ever more often as that expiry nears.
func renewalExtends(leaf, signer *x509.Certificate, now time.Time, ttl time.Duration) bool {
	return pki.RenewalExtends(min(ttl, signer.NotAfter.Sub(now)), leaf.NotAfter.Sub(now))
}

func sameNames(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// leafCertificate returns the first certificate of the Secret's tls.crt, or
// nil when there is none or it doesn't form a key pair with tls.key.
func leafCertificate(secret *corev1.Secret) *x509.Certificate {
	if secret == nil {
		return nil
	}
	return pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
}

func encodeCertificates(certs []*x509.Certificate) []byte {
	var buf bytes.Buffer
	for _, c := range certs {
		_ = pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return buf.Bytes()
}

// union returns the certificates of all the sets, each once, in order.
func union(sets ...[]*x509.Certificate) []*x509.Certificate {
	var out []*x509.Certificate
	for _, set := range sets {
		for _, c := range set {
			if !containsCertificate(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

// unexpired returns the certificates still valid at now.
func unexpired(certs []*x509.Certificate, now time.Time) []*x509.Certificate {
	return slices.DeleteFunc(slices.Clone(certs), func(c *x509.Certificate) bool { return !now.Before(c.NotAfter) })
}

func containsCertificate(certs []*x509.Certificate, cert *x509.Certificate) bool {
	return slices.ContainsFunc(certs, func(c *x509.Certificate) bool { return c.Equal(cert) })
}

// issuersOf returns the certificate authorities carried in the chain of the
// Secret's tls.crt that signed its certificate and are still valid.
func issuersOf(secret *corev1.Secret, now time.Time) []*x509.Certificate {
	leaf := leafCertificate(secret)
	if leaf == nil {
		return nil
	}
	var issuers []*x509.Certificate
	for _, c := range pki.ParseCertificates(secret.Data[corev1.TLSCertKey])[1:] {
		if c.IsCA && now.Before(c.NotAfter) && leaf.CheckSignatureFrom(c) == nil {
			issuers = append(issuers, c)
		}
	}
	return issuers
}

// anchorsOf returns the certificate authorities a peer may verify the
// server certificate of the Secret with: the still valid certificate
// authorities of the chain in its tls.crt and of its ca.crt that the
// certificate verifies with, or the certificate itself when self-signed.
func anchorsOf(secret *corev1.Secret, now time.Time) []*x509.Certificate {
	leaf := leafCertificate(secret)
	if leaf == nil {
		return nil
	}
	chain := pki.ParseCertificates(secret.Data[corev1.TLSCertKey])[1:]
	candidates := unexpired(union(chain, pki.ParseCertificates(secret.Data[consts.CACRT])), now)
	intermediates := x509.NewCertPool()
	for _, c := range chain {
		intermediates.AddCert(c)
	}
	// A certificate accepted within the clock skew tolerance before its
	// validity verifies as of its start.
	at := now
	if at.Before(leaf.NotBefore) {
		at = leaf.NotBefore
	}
	var anchors []*x509.Certificate
	for _, c := range candidates {
		if !c.IsCA {
			continue
		}
		roots := x509.NewCertPool()
		roots.AddCert(c)
		if _, err := leaf.Verify(x509.VerifyOptions{
			Roots: roots, Intermediates: intermediates, CurrentTime: at,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); err == nil {
			anchors = append(anchors, c)
		}
	}
	if len(anchors) == 0 && bytes.Equal(leaf.RawIssuer, leaf.RawSubject) &&
		leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil {
		anchors = append(anchors, leaf)
	}
	return anchors
}

// isManualCopy reports whether the Secret holds a copy of the user's Manual
// certificate authority or cp certificate.
func isManualCopy(secret *corev1.Secret) bool {
	return secret != nil && secret.Annotations[certificateSourceAnnotation] == certificateSourceManual
}

func bundleCerts(bundle *corev1.Secret) []*x509.Certificate {
	return pki.ParseCertificates(bundleData(bundle))
}

// ensureTLS provisions the TLS material of the GWAdmin, moving it one step
// forward when it is not up to date. deployment is the current gw-admin
// Deployment, or nil when the GWAdmin has none. The material is returned
// even with an error, as far as it was read, so that the Deployment can
// keep being applied while the user fixes a stop. A trust domain that can't
// be provisioned for a reason the user has to fix doesn't hold the others
// back: they still renew and rotate.
func (r *Reconciler) ensureTLS(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	deployment *appsv1.Deployment,
) (*tlsMaterial, error) {
	now := r.clock()
	previousMessage := certificateMessageOf(gw)

	m := &tlsMaterial{}
	for _, s := range []struct {
		ref secretRef
		dst **corev1.Secret
	}{
		{r.local(gw, apiCABundleSecretName(gw)), &m.apiBundle},
		{r.local(gw, apiTLSSecretName(gw)), &m.api},
		{r.local(gw, cpTLSSecretName(gw)), &m.cp},
		{r.local(gw, cpClientCABundleSecretName(gw)), &m.clientBundle},
		{r.local(gw, cpServerCABundleSecretName(gw)), &m.serverBundle},
		{r.private(gwadminref.OperatorClientTLSSecretName(gw)), &m.client},
	} {
		secret, err := r.getOwnedSecret(ctx, gw, s.ref)
		if err != nil {
			return m, err
		}
		*s.dst = secret
	}

	// What the pods run, before any change below.
	state := rolloutOf(deployment)
	p := &pass{
		now:     now,
		running: deployment != nil,
		trusted: state.runs(consts.GWAdminTrustChecksumAnnotation, m.trustChecksum()),
		rolled:  state.runs(consts.GWAdminTLSChecksumAnnotation, m.checksum()),
	}
	p.rolloutDescription = state.description
	if p.rolloutDescription == "" {
		p.rolloutDescription = "waiting for the Deployment to roll out"
	}
	var dataPlanes []dataPlane
	if gw.Annotations[consts.GWAdminRotationSkipDataPlanesAnnotation] == "true" {
		m.warnings = append(m.warnings, fmt.Sprintf("rotations don't wait for the data planes (%s): "+
			"data planes may fail to connect until they roll out", consts.GWAdminRotationSkipDataPlanesAnnotation))
	} else {
		var err error
		if dataPlanes, err = r.dataPlanes(ctx, gw); err != nil {
			return m, err
		}
	}

	// A stop in one trust domain is reported, and the others go on.
	var stop error
	keepGoing := func(err error) (bool, error) {
		if !errors.Is(err, errStop) {
			return err == nil, err
		}
		if stop == nil {
			stop = err
		}
		return false, nil
	}
	api, err := r.ensureAPIDomain(ctx, gw, m, p)
	if _, err := keepGoing(err); err != nil {
		return m, err
	}
	if _, err := keepGoing(r.ensureCPClientDomain(ctx, gw, m, p, dataPlanes)); err != nil {
		return m, err
	}
	server, err := r.ensureCPServerDomain(ctx, gw, m, p, dataPlanes)
	serverOK, err := keepGoing(err)
	if err != nil {
		return m, err
	}
	// Last: dropping trust waits for the pods to run everything above.
	if api != nil {
		if err := r.trimAPIBundle(ctx, gw, m, p, api); err != nil {
			return m, err
		}
	}
	if serverOK {
		if err := r.trimServerBundle(ctx, gw, m, p, server); err != nil {
			return m, err
		}
	}

	for _, w := range m.expiring {
		if !strings.Contains(previousMessage, w) {
			r.event(gw, corev1.EventTypeWarning, "CertificateExpiringSoon", "ValidateCertificate", "%s", w)
		}
	}

	// Look again when the next certificate is due. A moment already past is
	// waiting on something else, e.g. a rollout, whose events trigger the
	// reconcile.
	m.due = append(m.due, now.Add(pki.MaxRenewalRequeue))
	for _, l := range []*corev1.Secret{m.api, m.cp, m.client} {
		if c := leafCertificate(l); c != nil && !isManualCopy(l) {
			m.due = append(m.due, renewalTime(c))
		}
	}
	m.due = slices.DeleteFunc(m.due, func(t time.Time) bool { return !t.After(now) })
	m.requeueAfter = max(slices.MinFunc(m.due, time.Time.Compare).Sub(now), time.Second)
	if stop != nil {
		// The stop is in the condition already: look again when the stop
		// asks to, or when the other domains need it, whichever comes first.
		return m, &stopError{after: min(stopRequeue(stop), m.requeueAfter)}
	}

	warnings := ""
	if len(m.warnings) > 0 {
		warnings = "; " + strings.Join(m.warnings, "; ")
	}
	switch {
	case len(m.blocked) > 0:
		setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonCARotationBlocked,
			strings.Join(m.blocked, "; ")+warnings)
	case m.rotating:
		msg := "Certificates are moving to a new certificate authority"
		progress := m.progress
		if !p.rolled {
			progress = append(progress, p.rolloutDescription)
		}
		if len(progress) > 0 {
			msg += ": " + strings.Join(progress, "; ")
		}
		setCertificateCondition(gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCARotationInProgress, msg+warnings)
	default:
		setCertificateCondition(gw, metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned,
			"Certificates provisioned"+warnings)
	}
	return m, nil
}

// leafTarget is a certificate Kong Operator issues, and the Secret holding it.
type leafTarget struct {
	dst  **corev1.Secret
	ref  secretRef
	spec leafSpec
}

// apiDomain is the trust domain of the API listener.
type apiDomain struct {
	ca     *caRecord
	leaves []leafTarget
}

// ensureAPIDomain trusts the API CA, and the API CAs it replaced while
// certificates from them may be in use, in the API bundle, and issues the API
// server certificate and the operator client certificate from it once the
// pods trust it.
func (r *Reconciler) ensureAPIDomain(ctx context.Context, gw *operatorv1alpha1.GWAdmin, m *tlsMaterial, p *pass) (*apiDomain, error) {
	ca, err := r.ensureAutomaticCA(ctx, gw, r.private(APICASecretName(gw)), apiCACommonName(gw), p.now, func() []*x509.Certificate {
		return union(issuersOf(m.api, p.now), issuersOf(m.client, p.now))
	})
	if err != nil {
		return nil, err
	}
	d := &apiDomain{
		ca: ca,
		leaves: []leafTarget{
			{&m.api, r.local(gw, apiTLSSecretName(gw)), serverLeafSpec(apiServiceName(gw), gw.Namespace, r.ClusterDomain)},
			{&m.client, r.private(gwadminref.OperatorClientTLSSecretName(gw)), operatorClientLeafSpec()},
		},
	}
	m.due = append(m.due, renewalTime(ca.cert))

	// Phase 1: trust the signer, and the ones it replaced.
	before := bundleCerts(m.apiBundle)
	certs := ca.trusted()
	changed, err := r.writeBundle(ctx, gw, r.local(gw, apiCABundleSecretName(gw)), &m.apiBundle, certs)
	if err != nil {
		return nil, err
	}
	if changed {
		p.bundleChanged()
		r.trustEvent(ctx, gw, apiCABundleSecretName(gw), before, certs)
	}

	// Phase 2: issue the certificates from the signer once the pods trust it.
	for _, l := range d.leaves {
		issued, err := r.issueLeaf(ctx, gw, m, ca.secret, ca.cert, l, p)
		if err != nil {
			return nil, err
		}
		if issued && l.spec.mounted {
			p.leafChanged()
		}
	}
	if len(certs) > 1 || !d.allOnSigner() {
		m.rotating = true
	}
	return d, nil
}

// allOnSigner reports whether every certificate of the domain is signed by
// its signer.
func (d *apiDomain) allOnSigner() bool {
	for _, l := range d.leaves {
		if c := leafCertificate(*l.dst); c == nil || c.CheckSignatureFrom(d.ca.cert) != nil {
			return false
		}
	}
	return true
}

// trimAPIBundle stops trusting the previous API CAs once every certificate
// moved to the signer and the pods run them.
func (r *Reconciler) trimAPIBundle(ctx context.Context, gw *operatorv1alpha1.GWAdmin, m *tlsMaterial, p *pass, d *apiDomain) error {
	if len(d.ca.previous) == 0 || !d.allOnSigner() || !p.rolled {
		return nil
	}
	d.ca.previous = nil
	if err := r.saveRecord(ctx, gw, d.ca); err != nil {
		return err
	}
	changed, err := r.writeBundle(ctx, gw, r.local(gw, apiCABundleSecretName(gw)), &m.apiBundle, d.ca.trusted())
	if err != nil || !changed {
		return err
	}
	p.bundleChanged()
	r.trimEvent(ctx, gw, apiCABundleSecretName(gw))
	return nil
}

// issueLeaf issues the certificate l from signer when it is due. It reports
// whether the certificate was issued.
func (r *Reconciler) issueLeaf(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	m *tlsMaterial,
	signerSecret *corev1.Secret,
	signer *x509.Certificate,
	l leafTarget,
	p *pass,
) (bool, error) {
	leaf := leafCertificate(*l.dst)
	issue := leafNeedsIssue(*l.dst, l.spec, signer, p.now, p.running, r.certTTL())
	if !issue.needed {
		return false, nil
	}
	if issue.gated && !p.trusted && (leaf == nil || p.now.Before(leaf.NotAfter)) {
		if leaf != nil {
			m.due = append(m.due, renewalTime(leaf), leaf.NotAfter)
			if !p.now.Before(renewalTime(leaf)) {
				m.blocked = append(m.blocked, fmt.Sprintf("%s expires at %s, and can only move to the new certificate "+
					"authority once the pods roll out: %s", l.ref.name, leaf.NotAfter.UTC().Format(time.RFC3339), p.rolloutDescription))
			}
		}
		return false, nil
	}
	if err := r.issueInto(ctx, gw, signerSecret, signer, l); err != nil {
		return false, err
	}
	why := issueReason(leaf, signer, p.now)
	log.Debug(log.GetLogger(ctx, ControllerName, r.LoggingMode), "issued certificate", "secret", l.ref.name, "reason", why)
	r.event(gw, corev1.EventTypeNormal, "CertificateIssued", "IssueCertificate", "Issued certificate %s (%s)", l.ref.name, why)
	return true, nil
}

// issueInto issues the certificate l from signer and writes it, with its
// issuer, into its Secret.
func (r *Reconciler) issueInto(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	signerSecret *corev1.Secret,
	signer *x509.Certificate,
	l leafTarget,
) error {
	certPEM, keyPEM, err := issueCertificate(signerSecret, certRequest{
		commonName: l.spec.commonName,
		dnsNames:   l.spec.dnsNames,
		usages:     l.spec.usages,
		ttl:        r.certTTL(),
		now:        r.clock(),
	})
	if err != nil {
		return fmt.Errorf("failed issuing certificate %s: %w", l.ref.name, err)
	}
	// Carry the issuer along: peers get the full chain, and the bundle can
	// always be rebuilt from the certificates in use.
	certPEM = append(certPEM, encodeCertificates([]*x509.Certificate{signer})...)
	*l.dst, err = r.writeSecret(ctx, gw, *l.dst, l.ref, corev1.SecretTypeTLS,
		map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}, nil)
	return err
}

// issueReason tells why a certificate replacing leaf was issued from signer.
func issueReason(leaf, signer *x509.Certificate, now time.Time) string {
	switch {
	case leaf == nil:
		return "missing"
	case !now.Before(leaf.NotAfter):
		return "expired"
	case leaf.CheckSignatureFrom(signer) != nil:
		return "new certificate authority"
	case !now.Before(renewalTime(leaf)):
		return "renewal"
	default:
		return "names changed"
	}
}

// cpClientCA is the cp client CA: the one published in gwadminref.CPClientCASecretName,
// and the signer it moves to.
type cpClientCA struct {
	// published is the Secret named gwadminref.CPClientCASecretName, or nil, and
	// publishedCA its certificate authority while usable.
	published   *corev1.Secret
	publishedCA *x509.Certificate
	// source is the Secret the signer is read from: published, the next
	// Automatic one or the user's.
	source *corev1.Secret
	signer *x509.Certificate
	// lost holds what the cp client bundle trusted when the published
	// Secret was lost: the data planes may hold certificates from them.
	lost []*x509.Certificate
}

// isPublished reports whether the signer is the published certificate
// authority.
func (c *cpClientCA) isPublished(gw *operatorv1alpha1.GWAdmin) bool {
	return c.published != nil &&
		isManualCopy(c.published) == isManualCA(gw) &&
		bytes.Equal(c.published.Data[corev1.TLSCertKey], c.source.Data[corev1.TLSCertKey]) &&
		bytes.Equal(c.published.Data[corev1.TLSPrivateKeyKey], c.source.Data[corev1.TLSPrivateKeyKey])
}

// previous returns the certificate authorities published before the
// published one that the data planes may still use: those the published
// Secret records, or, when it was lost, those the bundle trusted.
func (c *cpClientCA) previous(now time.Time) []*x509.Certificate {
	if c.published == nil {
		return c.lost
	}
	return slices.DeleteFunc(unexpired(pki.ParseCertificates(c.published.Data[gwadminref.PreviousCAKey]), now),
		func(p *x509.Certificate) bool { return !p.IsCA })
}

// ensureCPClientDomain makes the pods trust the cp client CA, publishes it
// once they do, and keeps trusting the certificate authorities published
// before it until every data plane runs a certificate from the published
// one. The bundle is derived from the published Secret, in the operator
// namespace: what the bundle Secret holds is never trusted again.
func (r *Reconciler) ensureCPClientDomain(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	m *tlsMaterial,
	p *pass,
	dataPlanes []dataPlane,
) error {
	c, err := r.ensureClientSigner(ctx, gw, m, p)
	if err != nil {
		return err
	}
	ref := r.local(gw, cpClientCABundleSecretName(gw))
	before := bundleCerts(m.clientBundle)

	// Publish the signer once every pod trusts it: it is in the bundle they
	// run.
	if !c.isPublished(gw) && ((p.trusted && containsCertificate(before, c.signer)) || !p.running) {
		if err := r.publishClientCA(ctx, gw, c, p.now); err != nil {
			return err
		}
	}

	// The signer, the published certificate authority, and those published
	// before it until every data plane runs a certificate from the published
	// one. Certificate authorities that were never published are left out: no
	// data plane certificate can come from them.
	desired := []*x509.Certificate{c.signer}
	if c.publishedCA != nil {
		desired = union(desired, []*x509.Certificate{c.publishedCA})
	}
	waiting := "a certificate authority is published"
	if c.publishedCA != nil {
		waiting = dataPlanesRun(dataPlanes, consts.GWAdminDataPlaneClientCAAnnotation, pki.Fingerprint(c.publishedCA))
	}
	if waiting != "" {
		desired = union(desired, c.previous(p.now))
	} else if c.published != nil && len(c.published.Data[gwadminref.PreviousCAKey]) > 0 {
		// Every data plane moved: forget the previous ones, so that they are
		// never trusted again.
		if err := r.forgetPreviousClientCAs(ctx, gw, c); err != nil {
			return err
		}
	}
	desired = unexpired(desired, p.now)
	if changed, err := r.writeBundle(ctx, gw, ref, &m.clientBundle, desired); err != nil {
		return err
	} else if changed {
		p.bundleChanged()
		if len(desired) < len(before) && len(desired) == 1 {
			r.trimEvent(ctx, gw, ref.name)
		} else {
			r.trustEvent(ctx, gw, ref.name, before, desired)
		}
	}

	switch {
	case !c.isPublished(gw):
		m.rotating = true
		m.progress = append(m.progress, "the new cp client CA is published once the pods trust it")
	case len(desired) > 1:
		m.rotating = true
		m.progress = append(m.progress, fmt.Sprintf("the previous cp client CA stays trusted until every data plane "+
			"runs a certificate issued from the new one: %s", waiting))
	}
	return nil
}

// forgetPreviousClientCAs removes the certificate authorities published
// before the published one from the published Secret.
func (r *Reconciler) forgetPreviousClientCAs(ctx context.Context, gw *operatorv1alpha1.GWAdmin, c *cpClientCA) error {
	published, err := r.writeSecret(ctx, gw, c.published, r.private(gwadminref.CPClientCASecretName(gw)), corev1.SecretTypeTLS,
		map[string][]byte{
			corev1.TLSCertKey:       c.published.Data[corev1.TLSCertKey],
			corev1.TLSPrivateKeyKey: c.published.Data[corev1.TLSPrivateKeyKey],
		}, keptAnnotations(c.published))
	if err != nil {
		return err
	}
	if c.source == c.published {
		c.source = published
	}
	c.published = published
	return nil
}

// keptAnnotations returns the annotations Kong Operator manages on secret, to
// write them back unchanged.
func keptAnnotations(secret *corev1.Secret) map[string]string {
	annotations := map[string]string{}
	for _, k := range managedAnnotations {
		if v, ok := secret.Annotations[k]; ok {
			annotations[k] = v
		}
	}
	return annotations
}

// ensureClientSigner returns the cp client CA. The signer is the user's when
// Manual. When Automatic, it is the published one until it is due for
// renewal or rotated; then a new one is created in cpClientCANextSecretName,
// to be published once the pods trust it.
func (r *Reconciler) ensureClientSigner(ctx context.Context, gw *operatorv1alpha1.GWAdmin, m *tlsMaterial, p *pass) (*cpClientCA, error) {
	published, err := r.getOwnedSecret(ctx, gw, r.private(gwadminref.CPClientCASecretName(gw)))
	if err != nil {
		return nil, err
	}
	c := &cpClientCA{published: published}
	c.publishedCA, _ = validateCA(published, p.now)
	if published == nil {
		// Lost, or not published yet: the data planes may hold certificates
		// from what the bundle trusted.
		c.lost = slices.DeleteFunc(unexpired(bundleCerts(m.clientBundle), p.now),
			func(x *x509.Certificate) bool { return !x.IsCA })
	}
	nextRef := r.private(cpClientCANextSecretName(gw))

	if isManualCA(gw) {
		if c.source, c.signer, err = r.manualSigner(ctx, gw, p.now); err != nil {
			return nil, err
		}
		m.due = append(m.due, warningTime(c.signer), c.signer.NotAfter)
		if !p.now.Before(warningTime(c.signer)) {
			m.warnExpiring(fmt.Sprintf("CA Secret %q expires at %s",
				gw.Spec.TLS.ClientCA.SecretRef.Name, c.signer.NotAfter.UTC().Format(time.RFC3339)))
		}
		return c, r.deleteOwnedSecret(ctx, gw, nextRef)
	}
	if c.publishedCA != nil && !isManualCopy(published) && p.now.Before(renewalTime(c.publishedCA)) &&
		!rotationRequested(gw, published) {
		c.source, c.signer = published, c.publishedCA
		m.due = append(m.due, renewalTime(c.signer))
		return c, r.deleteOwnedSecret(ctx, gw, nextRef)
	}
	next, err := r.getOwnedSecret(ctx, gw, nextRef)
	if err != nil {
		return nil, err
	}
	if !p.running && next == nil {
		// No pod to wait for: the new certificate authority is published
		// right away, without going through cpClientCANextSecretName.
		certPEM, keyPEM, err := generateCA(clientCACommonName(gw), p.now, r.caTTL())
		if err != nil {
			return nil, err
		}
		c.source = &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}}
		c.signer, err = validateCA(c.source, p.now)
		return c, err
	}
	ca, err := r.ensureAutomaticCA(ctx, gw, nextRef, clientCACommonName(gw), p.now, nil)
	if err != nil {
		return nil, err
	}
	c.source, c.signer = ca.secret, ca.cert
	return c, nil
}

// publishClientCA copies the signer into gwadminref.CPClientCASecretName, recording the
// certificate authorities published before it, which the data planes may
// still use.
func (r *Reconciler) publishClientCA(ctx context.Context, gw *operatorv1alpha1.GWAdmin, c *cpClientCA, now time.Time) error {
	data := map[string][]byte{
		corev1.TLSCertKey:       c.source.Data[corev1.TLSCertKey],
		corev1.TLSPrivateKeyKey: c.source.Data[corev1.TLSPrivateKeyKey],
	}
	previous := c.previous(now)
	if c.publishedCA != nil {
		previous = union([]*x509.Certificate{c.publishedCA}, previous)
	}
	previous = slices.DeleteFunc(previous, func(p *x509.Certificate) bool { return p.Equal(c.signer) })
	if len(previous) > 0 {
		data[gwadminref.PreviousCAKey] = encodeCertificates(previous)
	}
	annotations := map[string]string{caPublishedAtAnnotation: now.UTC().Format(time.RFC3339)}
	if isManualCA(gw) {
		annotations[certificateSourceAnnotation] = certificateSourceManual
	} else {
		annotations[rotationTokenAnnotation] = rotationToken(gw)
	}
	published, err := r.writeSecret(ctx, gw, c.published, r.private(gwadminref.CPClientCASecretName(gw)), corev1.SecretTypeTLS, data, annotations)
	if err != nil {
		return err
	}
	c.published, c.publishedCA = published, c.signer
	log.Info(log.GetLogger(ctx, ControllerName, r.LoggingMode), "published the cp client CA", "secret", published.Name)
	r.event(gw, corev1.EventTypeNormal, "CAPublished", "PublishCA",
		"Published certificate authority %s in %s", c.signer.Subject.CommonName, published.Name)
	if c.source.Name == cpClientCANextSecretName(gw) {
		c.source = published
		return r.deleteOwnedSecret(ctx, gw, r.private(cpClientCANextSecretName(gw)))
	}
	if !isManualCA(gw) {
		c.source = published
	}
	return nil
}

// serverDomain is the trust domain the data planes verify the cp listener
// with.
type serverDomain struct {
	ca *caRecord
	// targetAnchors are the issuers of the certificate the cp listener must
	// serve.
	targetAnchors []*x509.Certificate
}

// trusted returns what the cp server bundle trusts: the cp server CA, the
// issuers of the certificate served and of the one to serve, and the issuers
// it replaced while the pods may still serve certificates from them.
func (d *serverDomain) trusted() []*x509.Certificate {
	return union(d.ca.trusted(), d.ca.served, d.targetAnchors)
}

// ensureCPServerDomain trusts the issuers of the cp server certificate in
// the cp server bundle, and moves the certificate to a new issuer once every
// data plane runs that bundle. The bundle is derived from what the cp server
// CA Secret, in the operator namespace, records, and from the user's Manual
// certificate: what the bundle or the cp certificate Secret hold is never
// trusted again.
func (r *Reconciler) ensureCPServerDomain(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	m *tlsMaterial,
	p *pass,
	dataPlanes []dataPlane,
) (*serverDomain, error) {
	// The cp server CA always exists and is always trusted, so that a switch
	// from a Manual cp certificate back to an Automatic one is immediate.
	ca, err := r.ensureAutomaticCA(ctx, gw, r.private(CPServerCASecretName(gw)), cpServerCACommonName(gw), p.now,
		func() []*x509.Certificate { return anchorsOf(m.cp, p.now) })
	if err != nil {
		return nil, err
	}
	if !ca.servedKnown {
		// Not recorded yet, or lost with the cp server CA Secret: the
		// issuers of the certificate in place.
		ca.served = anchorsOf(m.cp, p.now)
	}
	m.due = append(m.due, renewalTime(ca.cert))
	d := &serverDomain{ca: ca, targetAnchors: []*x509.Certificate{ca.cert}}
	var manual *corev1.Secret
	if isManualCP(gw) {
		if manual, err = r.manualCPCertificate(ctx, gw, m, p.now); err != nil {
			return nil, err
		}
		d.targetAnchors = anchorsOf(manual, p.now)
	}

	// Phase 1: trust the issuers of the certificate served and of the one to
	// serve.
	ref := r.local(gw, cpServerCABundleSecretName(gw))
	before := bundleCerts(m.serverBundle)
	certs := d.trusted()
	if changed, err := r.writeBundle(ctx, gw, ref, &m.serverBundle, certs); err != nil {
		return nil, err
	} else if changed {
		r.trustEvent(ctx, gw, ref.name, before, certs)
	}

	// Phase 2: serve the certificate once every data plane runs that
	// bundle, unless the one in use is missing or expired, or shares an
	// issuer with it: the data planes verify the one in use with that
	// issuer, so they trust the new one already.
	current := leafCertificate(m.cp)
	l := leafTarget{&m.cp, r.local(gw, cpTLSSecretName(gw)), serverLeafSpec(cpServiceName(gw), gw.Namespace, r.ClusterDomain)}
	var needed bool
	switch {
	case manual != nil:
		needed = !sameCertificateData(m.cp, manual)
	case isManualCopy(m.cp):
		needed = true
	default:
		needed = leafNeedsIssue(m.cp, l.spec, ca.cert, p.now, false, r.certTTL()).needed
	}
	if !needed {
		return d, r.recordServed(ctx, gw, d)
	}
	trustedAlready := slices.ContainsFunc(ca.served, func(a *x509.Certificate) bool {
		return containsCertificate(d.targetAnchors, a)
	})
	if waiting := dataPlanesRun(dataPlanes, consts.GWAdminDataPlaneServerBundleAnnotation,
		gwadminref.DataPlaneServerBundleChecksum(m.serverBundle)); waiting != "" && !trustedAlready && current != nil && p.now.Before(current.NotAfter) {
		m.rotating = true
		m.due = append(m.due, current.NotAfter)
		if !p.now.Before(renewalTime(current)) {
			m.blocked = append(m.blocked, fmt.Sprintf("%s expires at %s, and can only move to its new issuer once every "+
				"data plane runs %s: %s", l.ref.name, current.NotAfter.UTC().Format(time.RFC3339), ref.name, waiting))
		} else {
			m.progress = append(m.progress, fmt.Sprintf("the cp server certificate moves to its new issuer once every "+
				"data plane runs %s: %s", ref.name, waiting))
		}
		return d, r.recordServed(ctx, gw, d)
	}
	if manual != nil {
		data := map[string][]byte{
			corev1.TLSCertKey:       manual.Data[corev1.TLSCertKey],
			corev1.TLSPrivateKeyKey: manual.Data[corev1.TLSPrivateKeyKey],
		}
		if ca := manual.Data[consts.CACRT]; len(ca) > 0 {
			data[consts.CACRT] = ca
		}
		if m.cp, err = r.writeSecret(ctx, gw, m.cp, l.ref, corev1.SecretTypeTLS, data,
			map[string]string{certificateSourceAnnotation: certificateSourceManual}); err != nil {
			return nil, err
		}
		r.event(gw, corev1.EventTypeNormal, "ManualCertificateCopied", "CopyCertificate",
			"Copied cp certificate Secret %s into %s", gw.Spec.TLS.CP.SecretRef.Name, l.ref.name)
	} else {
		if err := r.issueInto(ctx, gw, ca.secret, ca.cert, l); err != nil {
			return nil, err
		}
		r.event(gw, corev1.EventTypeNormal, "CertificateIssued", "IssueCertificate",
			"Issued certificate %s (%s)", l.ref.name, issueReason(current, ca.cert, p.now))
	}
	p.leafChanged()
	// The pods may still serve the previous certificate until they roll out:
	// its issuers stay trusted until then.
	ca.previous = union(ca.previous, slices.DeleteFunc(slices.Clone(ca.served), func(a *x509.Certificate) bool {
		return containsCertificate(d.targetAnchors, a)
	}))
	ca.served, ca.servedKnown = d.targetAnchors, true
	return d, r.saveRecord(ctx, gw, ca)
}

// recordServed records, on the cp server CA Secret, the issuers of the
// certificate the cp listener serves, when they weren't yet.
func (r *Reconciler) recordServed(ctx context.Context, gw *operatorv1alpha1.GWAdmin, d *serverDomain) error {
	d.ca.servedKnown = true
	return r.saveRecord(ctx, gw, d.ca)
}

// trimServerBundle stops trusting the issuers of certificates the cp
// listener no longer serves, once every pod serves the current one.
func (r *Reconciler) trimServerBundle(ctx context.Context, gw *operatorv1alpha1.GWAdmin, m *tlsMaterial, p *pass, d *serverDomain) error {
	if len(d.ca.previous) == 0 {
		return nil
	}
	if !p.rolled {
		m.rotating = true
		return nil
	}
	d.ca.previous = nil
	if err := r.saveRecord(ctx, gw, d.ca); err != nil {
		return err
	}
	ref := r.local(gw, cpServerCABundleSecretName(gw))
	changed, err := r.writeBundle(ctx, gw, ref, &m.serverBundle, d.trusted())
	if err != nil || !changed {
		return err
	}
	r.trimEvent(ctx, gw, ref.name)
	return nil
}

// dataPlane is a data plane Deployment of a GWAdmin, and how far it rolled
// out.
type dataPlane struct {
	name  string
	state rolloutState
}

// dataPlanes returns the data plane Deployments of the GWAdmin, sorted by
// namespace and name. Only those the owner of the GWAdmin allows count: in
// its namespace, or in a namespace a KongReferenceGrant in its namespace
// grants from OnPremEventGateway to GWAdmin. Anyone else's labelled
// Deployment is ignored: it can't hold the rotations back.
func (r *Reconciler) dataPlanes(ctx context.Context, gw *operatorv1alpha1.GWAdmin) ([]dataPlane, error) {
	list := &appsv1.DeploymentList{}
	if err := r.List(ctx, list, client.MatchingLabels{
		consts.GWAdminDataPlaneNamespaceLabel: gw.Namespace,
		consts.GWAdminDataPlaneNameLabel:      gw.Name,
	}); err != nil {
		return nil, fmt.Errorf("failed to list the data planes of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	granted, err := r.grantedNamespaces(ctx, gw)
	if err != nil {
		return nil, err
	}
	dps := make([]dataPlane, 0, len(list.Items))
	for i := range list.Items {
		d := &list.Items[i]
		if d.Namespace != gw.Namespace && !granted[d.Namespace] {
			continue
		}
		dps = append(dps, dataPlane{name: d.Namespace + "/" + d.Name, state: rolloutOf(d)})
	}
	slices.SortFunc(dps, func(a, b dataPlane) int { return strings.Compare(a.name, b.name) })
	return dps, nil
}

// grantedNamespaces returns the namespaces the KongReferenceGrants in the
// namespace of the GWAdmin allow its data planes in: those of the
// OnPremEventGateways they grant to reference it.
func (r *Reconciler) grantedNamespaces(ctx context.Context, gw *operatorv1alpha1.GWAdmin) (map[string]bool, error) {
	grants := &configurationv1alpha1.KongReferenceGrantList{}
	if err := r.List(ctx, grants, client.InNamespace(gw.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list the KongReferenceGrants of namespace %s: %w", gw.Namespace, err)
	}
	granted := map[string]bool{}
	for _, g := range grants.Items {
		if !slices.ContainsFunc(g.Spec.To, func(to configurationv1alpha1.ReferenceGrantTo) bool {
			return string(to.Group) == operatorv1alpha1.SchemeGroupVersion.Group && string(to.Kind) == operatorv1alpha1.GWAdminKind &&
				(to.Name == nil || string(*to.Name) == gw.Name)
		}) {
			continue
		}
		for _, from := range g.Spec.From {
			if string(from.Group) == eventgatewayv1alpha1.GroupName && string(from.Kind) == onPremEventGatewayKind {
				granted[string(from.Namespace)] = true
			}
		}
	}
	return granted, nil
}

// onPremEventGatewayKind is the kind of the resources whose namespace a
// KongReferenceGrant allows the data planes of a GWAdmin in.
const onPremEventGatewayKind = "OnPremEventGateway"

// dataPlanesRun returns "" when every data plane runs the Pod template
// annotation key set to value, or tells which one doesn't.
func dataPlanesRun(dps []dataPlane, key, value string) string {
	for _, dp := range dps {
		switch {
		case dp.state.runs(key, value):
		case dp.state.complete:
			return fmt.Sprintf("data plane Deployment %s doesn't run it yet", dp.name)
		default:
			return fmt.Sprintf("data plane Deployment %s: %s", dp.name, dp.state.description)
		}
	}
	return ""
}

func bundleData(bundle *corev1.Secret) []byte {
	if bundle == nil {
		return nil
	}
	return bundle.Data[consts.CACRT]
}

func sameCertificateData(a, b *corev1.Secret) bool {
	return a != nil && b != nil &&
		bytes.Equal(a.Data[corev1.TLSCertKey], b.Data[corev1.TLSCertKey]) &&
		bytes.Equal(a.Data[corev1.TLSPrivateKeyKey], b.Data[corev1.TLSPrivateKeyKey]) &&
		bytes.Equal(a.Data[consts.CACRT], b.Data[consts.CACRT])
}

// writeBundle writes certs into the bundle Secret when it doesn't hold
// exactly them. It reports whether it wrote.
func (r *Reconciler) writeBundle(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	ref secretRef,
	bundle **corev1.Secret,
	certs []*x509.Certificate,
) (bool, error) {
	data := encodeCertificates(certs)
	if b := *bundle; b != nil && bytes.Equal(b.Data[consts.CACRT], data) && len(b.Data) == 1 {
		return false, nil
	}
	var err error
	*bundle, err = r.writeSecret(ctx, gw, *bundle, ref, corev1.SecretTypeOpaque, map[string][]byte{consts.CACRT: data}, nil)
	return err == nil, err
}

// trustEvent reports that a bundle started trusting a new certificate
// authority, unless it was just created.
func (r *Reconciler) trustEvent(ctx context.Context, gw *operatorv1alpha1.GWAdmin, name string, before, after []*x509.Certificate) {
	if len(before) == 0 || len(after) <= len(before) {
		return
	}
	log.Info(log.GetLogger(ctx, ControllerName, r.LoggingMode), "trusting a new certificate authority", "bundle", name, "size", len(after))
	r.event(gw, corev1.EventTypeNormal, "CARotationStarted", "UpdateTrustBundle",
		"Trust bundle %s now holds %d certificate authorities", name, len(after))
}

// trimEvent reports that a bundle stopped trusting the previous certificate
// authorities.
func (r *Reconciler) trimEvent(ctx context.Context, gw *operatorv1alpha1.GWAdmin, name string) {
	log.Info(log.GetLogger(ctx, ControllerName, r.LoggingMode), "dropped the previous certificate authorities", "bundle", name)
	r.event(gw, corev1.EventTypeNormal, "CARotationCompleted", "UpdateTrustBundle",
		"Trust bundle %s no longer holds the previous certificate authorities", name)
}

// caRecord is a certificate authority Kong Operator owns, in the operator
// namespace, and what its Secret records about the trust bundle of its
// domain, so that the bundle is never derived from what the namespace of the
// GWAdmin holds.
type caRecord struct {
	ref    secretRef
	secret *corev1.Secret
	cert   *x509.Certificate
	// previous are the certificate authorities, or issuers, the domain moves
	// off: peers may still verify certificates from them.
	previous []*x509.Certificate
	// served are, for the cp server CA, the issuers of the certificate the cp
	// listener serves. servedKnown is false while they aren't recorded.
	served      []*x509.Certificate
	servedKnown bool
}

// trusted returns the certificate authority and those it replaced.
func (c *caRecord) trusted() []*x509.Certificate {
	return union([]*x509.Certificate{c.cert}, c.previous)
}

// ensureAutomaticCA returns the certificate authority Kong Operator owns at
// ref. It is created, or renewed in place once due or rotated through the
// GWAdminRotateCAsAnnotation; the certificate authority it replaces is then
// recorded as previous. A domain that records what its bundle trusts passes
// lost: when the Secret is created, e.g. after it was lost, it returns the
// issuers of the certificates in use, which are recorded as previous.
func (r *Reconciler) ensureAutomaticCA(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	ref secretRef,
	commonName string,
	now time.Time,
	lost func() []*x509.Certificate,
) (*caRecord, error) {
	secret, err := r.getOwnedSecret(ctx, gw, ref)
	if err != nil {
		return nil, err
	}
	rec := &caRecord{ref: ref, secret: secret}
	if secret != nil && lost != nil {
		rec.previous = unexpired(pki.ParseCertificates(secret.Data[gwadminref.PreviousCAKey]), now)
		if served, ok := secret.Data[servedCAKey]; ok {
			rec.served, rec.servedKnown = unexpired(pki.ParseCertificates(served), now), true
		}
	}
	ca, err := validateCA(secret, now)
	if err == nil && now.Before(renewalTime(ca)) && !rotationRequested(gw, secret) {
		rec.cert = ca
		return rec, nil
	}
	switch {
	case lost == nil:
	case ca != nil:
		rec.previous = union(rec.previous, []*x509.Certificate{ca})
	default:
		rec.previous = union(rec.previous, lost())
	}
	certPEM, keyPEM, err := generateCA(commonName, now, r.caTTL())
	if err != nil {
		return nil, err
	}
	renewed := secret != nil
	if rec.secret, err = r.writeSecret(ctx, gw, secret, ref, corev1.SecretTypeTLS, rec.data(certPEM, keyPEM),
		map[string]string{rotationTokenAnnotation: rotationToken(gw)}); err != nil {
		return nil, err
	}
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)
	if renewed {
		log.Info(logger, "renewed the certificate authority", "secret", ref.name)
		r.event(gw, corev1.EventTypeNormal, "CARenewed", "RenewCA", "Renewed certificate authority %s", commonName)
	} else {
		log.Info(logger, "created the certificate authority", "secret", ref.name)
		r.event(gw, corev1.EventTypeNormal, "CACreated", "CreateCA", "Created certificate authority %s", commonName)
	}
	rec.cert, err = validateCA(rec.secret, now)
	return rec, err
}

// data returns the data of the Secret of the record, holding the certificate
// authority in certPEM and keyPEM.
func (c *caRecord) data(certPEM, keyPEM []byte) map[string][]byte {
	data := map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}
	if len(c.previous) > 0 {
		data[gwadminref.PreviousCAKey] = encodeCertificates(c.previous)
	}
	if c.servedKnown {
		data[servedCAKey] = encodeCertificates(c.served)
	}
	return data
}

// saveRecord writes what the record holds into its Secret.
func (r *Reconciler) saveRecord(ctx context.Context, gw *operatorv1alpha1.GWAdmin, c *caRecord) error {
	data := c.data(c.secret.Data[corev1.TLSCertKey], c.secret.Data[corev1.TLSPrivateKeyKey])
	if maps.EqualFunc(data, c.secret.Data, bytes.Equal) {
		return nil
	}
	secret, err := r.writeSecret(ctx, gw, c.secret, c.ref, corev1.SecretTypeTLS, data, keptAnnotations(c.secret))
	if err != nil {
		return err
	}
	c.secret = secret
	return nil
}

// clientCACommonName returns the common name of the cp client CA.
func clientCACommonName(gw *operatorv1alpha1.GWAdmin) string {
	return truncateCommonName(fmt.Sprintf("gw-admin cp client CA %s/%s", gw.Namespace, gw.Name))
}

// cpServerCACommonName returns the common name of the cp server CA.
func cpServerCACommonName(gw *operatorv1alpha1.GWAdmin) string {
	return truncateCommonName(fmt.Sprintf("gw-admin cp server CA %s/%s", gw.Namespace, gw.Name))
}

// apiCACommonName returns the common name of the API CA.
func apiCACommonName(gw *operatorv1alpha1.GWAdmin) string {
	return truncateCommonName(fmt.Sprintf("gw-admin API CA %s/%s", gw.Namespace, gw.Name))
}

// truncateCommonName truncates a common name to the 64 characters RFC 5280
// allows.
func truncateCommonName(cn string) string {
	const maxLen = 64
	if len(cn) > maxLen {
		cn = cn[:maxLen]
	}
	return cn
}

// secretVisibilityHint tells how to make a Secret visible to the operator.
func (r *Reconciler) secretVisibilityHint() string {
	if r.SecretLabelSelector == "" {
		return ""
	}
	return fmt.Sprintf(" (it must exist and carry the %q label to be visible to the operator)", r.SecretLabelSelector)
}

// manualSigner returns the user's certificate authority Secret, once validated.
func (r *Reconciler) manualSigner(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	now time.Time,
) (*corev1.Secret, *x509.Certificate, error) {
	name := gw.Spec.TLS.ClientCA.SecretRef.Name
	secret, err := r.userSecret(ctx, gw, "CA", name)
	if err != nil {
		return nil, nil, err
	}
	ca, err := validateCA(secret, now)
	if err == nil {
		err = validateClientCA(ca)
	}
	if err != nil {
		setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonInvalidSecret,
			fmt.Sprintf("CA Secret %q is not usable: %v", name, err))
		if notYetValid, ok := errors.AsType[*notYetValidError](err); ok {
			return nil, nil, &stopError{after: notYetValid.validFrom.Sub(now)}
		}
		return nil, nil, errStop
	}
	return secret, ca, nil
}

// userSecret returns the named Secret the spec of the GWAdmin references, the
// what of the user. A Secret Kong Operator provisions is refused.
func (r *Reconciler) userSecret(ctx context.Context, gw *operatorv1alpha1.GWAdmin, what, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefNotFound,
				fmt.Sprintf("%s Secret %q not found%s", what, name, r.secretVisibilityHint()))
			return nil, errStop
		}
		// A transient API error or an RBAC denial: retried with backoff.
		setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretReadFailed,
			fmt.Sprintf("failed to read %s Secret %q: %v", what, name, err))
		return nil, fmt.Errorf("failed to read %s Secret %s/%s: %w", what, gw.Namespace, name, err)
	}
	if secret.Labels[consts.SecretProvisioningLabelKey] == consts.SecretProvisioningAutomaticLabelValue ||
		metav1.IsControlledBy(secret, gw) {
		setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefOperatorManaged,
			fmt.Sprintf("%s Secret %q is provisioned and managed by the operator; Manual provisioning requires a user-owned Secret", what, name))
		return nil, errStop
	}
	return secret, nil
}

// validateClientCA checks a Manual cp client CA can vouch for data plane
// client certificates with a key strong enough.
func validateClientCA(ca *x509.Certificate) error {
	if pub, ok := ca.PublicKey.(*rsa.PublicKey); ok && pub.N.BitLen() < minRSAKeyBits {
		return fmt.Errorf("its %d bits RSA key is too short, at least %d bits are required", pub.N.BitLen(), minRSAKeyBits)
	}
	if len(ca.ExtKeyUsage) > 0 &&
		!slices.Contains(ca.ExtKeyUsage, x509.ExtKeyUsageClientAuth) &&
		!slices.Contains(ca.ExtKeyUsage, x509.ExtKeyUsageAny) {
		return errors.New("its extended key usage excludes client authentication")
	}
	return nil
}

// deleteOwnedSecret deletes the GWAdmin's Secret at ref, when it exists.
func (r *Reconciler) deleteOwnedSecret(ctx context.Context, gw *operatorv1alpha1.GWAdmin, ref secretRef) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, ref.key(), secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	if !ownsSecret(gw, ref, secret) {
		return nil
	}
	if err := r.Delete(ctx, secret, client.Preconditions{UID: &secret.UID}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	log.Debug(log.GetLogger(ctx, ControllerName, r.LoggingMode), "deleted Secret", "secret", ref.name)
	return nil
}

// notYetValidError is returned by validateCA for a certificate authority
// that only becomes valid later.
type notYetValidError struct {
	validFrom time.Time
}

func (e *notYetValidError) Error() string {
	return fmt.Sprintf("the certificate is not valid before %s", e.validFrom.UTC().Format(time.RFC3339))
}

// validateCA checks the Secret holds a currently valid certificate authority
// in tls.crt, with its key in tls.key, and returns it. tls.crt must start
// with that certificate.
func validateCA(secret *corev1.Secret, now time.Time) (*x509.Certificate, error) {
	if secret == nil {
		return nil, errors.New("missing")
	}
	if first, _ := pem.Decode(secret.Data[corev1.TLSCertKey]); first == nil || first.Type != "CERTIFICATE" {
		return nil, errors.New("tls.crt must start with the certificate of the certificate authority")
	}
	ca := leafCertificate(secret)
	switch {
	case ca == nil:
		return nil, errors.New("tls.crt and tls.key must hold a certificate and its key")
	case ca.PublicKeyAlgorithm != x509.RSA && ca.PublicKeyAlgorithm != x509.ECDSA:
		return nil, fmt.Errorf("%s keys are not supported, only RSA and ECDSA ones", ca.PublicKeyAlgorithm)
	case !ca.IsCA:
		return nil, errors.New("the certificate is not a certificate authority")
	case ca.KeyUsage != 0 && ca.KeyUsage&x509.KeyUsageCertSign == 0:
		return nil, errors.New("the certificate can't sign certificates")
	case !now.Before(ca.NotAfter):
		return nil, fmt.Errorf("the certificate expired at %s", ca.NotAfter.UTC().Format(time.RFC3339))
	case now.Before(ca.NotBefore.Add(-clockSkew)):
		return nil, &notYetValidError{validFrom: ca.NotBefore.Add(-clockSkew)}
	}
	return ca, nil
}

// manualCPCertificate returns the user's cp server certificate Secret, once
// validated: a key pair valid at now, give or take clockSkew, the data
// planes can verify.
func (r *Reconciler) manualCPCertificate(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	m *tlsMaterial,
	now time.Time,
) (*corev1.Secret, error) {
	name := gw.Spec.TLS.CP.SecretRef.Name
	src, err := r.userSecret(ctx, gw, "cp certificate", name)
	if err != nil {
		return nil, err
	}
	leaf := leafCertificate(src)
	invalid := func(msg string) error {
		setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonInvalidSecret,
			fmt.Sprintf("cp certificate Secret %q %s", name, msg))
		return errStop
	}
	switch {
	case leaf == nil:
		return nil, invalid("does not contain a valid tls.crt/tls.key pair")
	case !now.Before(leaf.NotAfter):
		return nil, invalid("holds a certificate that expired at " + leaf.NotAfter.UTC().Format(time.RFC3339))
	case now.Before(leaf.NotBefore.Add(-clockSkew)):
		// No watch event fires when it becomes valid: look again then.
		return nil, errors.Join(invalid("holds a certificate not valid before "+leaf.NotBefore.UTC().Format(time.RFC3339)),
			&stopError{after: leaf.NotBefore.Add(-clockSkew).Sub(now)})
	case isManualCA(gw) && gw.Spec.TLS.ClientCA.SecretRef.Name == name:
		return nil, invalid("is the CA Secret: its private key would be mounted into the gw-admin pods")
	case leaf.IsCA:
		return nil, invalid("holds a certificate authority, not a server certificate")
	case len(leaf.ExtKeyUsage) > 0 &&
		!slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) &&
		!slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageAny):
		return nil, invalid("holds a certificate that can't be used by a server")
	case len(anchorsOf(src, now)) == 0:
		return nil, invalid("carries no issuer the data planes can verify it with: add its chain to tls.crt, or its root certificate authority to ca.crt")
	}
	if host := cpHost(gw, r.ClusterDomain); leaf.VerifyHostname(host) != nil {
		m.warnings = append(m.warnings, fmt.Sprintf("the cp certificate doesn't cover %s, the host in status.cp.serverURL", host))
	}
	m.due = append(m.due, warningTime(leaf), leaf.NotAfter)
	if !now.Before(warningTime(leaf)) {
		m.warnExpiring(fmt.Sprintf("cp certificate Secret %q expires at %s", name, leaf.NotAfter.UTC().Format(time.RFC3339)))
	}
	return src, nil
}

// getOwnedSecret returns the GWAdmin's Secret at ref, or nil when it doesn't
// exist. A Secret there that is not the GWAdmin's is reported and left alone.
func (r *Reconciler) getOwnedSecret(ctx context.Context, gw *operatorv1alpha1.GWAdmin, ref secretRef) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, ref.key(), secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	if !ownsSecret(gw, ref, secret) {
		return nil, r.secretConflict(gw, ref, secret)
	}
	return secret, nil
}

// secretConflict reports that a Secret the GWAdmin needs is someone else's.
func (r *Reconciler) secretConflict(gw *operatorv1alpha1.GWAdmin, ref secretRef, secret *corev1.Secret) error {
	msg := fmt.Sprintf("Secret %q already exists and is not managed by this GWAdmin: delete it, or rename it", ref.name)
	if secret != nil && ownedByPredecessor(secret, gw) {
		msg = fmt.Sprintf("Secret %q is still owned by a deleted GWAdmin named %q: waiting for the garbage collector to delete it",
			ref.name, gw.Name)
	}
	setCertificateCondition(gw, metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretConflict, msg)
	return &stopError{after: conflictRequeue}
}

// writeSecret creates the GWAdmin's Secret at ref with data, or updates
// existing in place when it is not nil. annotations replaces the annotations
// Kong Operator manages on the Secret.
func (r *Reconciler) writeSecret(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	existing *corev1.Secret,
	ref secretRef,
	secretType corev1.SecretType,
	data map[string][]byte,
	annotations map[string]string,
) (*corev1.Secret, error) {
	labels := k8sresources.GetManagedLabelForOwner(gw)
	labels[consts.GatewayOperatorManagedByNameLabel] = gw.Name
	labels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
	labels[consts.GWAdminUIDLabel] = string(gw.UID)
	if ref.private {
		labels[consts.GatewayOperatorManagedByNamespaceLabel] = gw.Namespace
	}
	if r.SecretLabelSelector != "" {
		labels[r.SecretLabelSelector] = "true"
	}

	if existing == nil {
		secret := &corev1.Secret{
			Namespace: ref.namespace, Name: ref.name, Labels: labels, Annotations: annotations,
			Type: secretType,
			Data: data,
		}
		if !ref.private {
			k8sutils.SetOwnerForObject(secret, gw)
		}
		if err := r.Create(ctx, secret); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil, r.resolveExistingSecret(ctx, gw, ref, labels)
			}
			return nil, fmt.Errorf("failed to create Secret %s/%s: %w", ref.namespace, ref.name, err)
		}
		return secret, nil
	}

	secret := existing.DeepCopy()
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	maps.Copy(secret.Labels, labels)
	for _, k := range managedAnnotations {
		delete(secret.Annotations, k)
	}
	if len(annotations) > 0 && secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	maps.Copy(secret.Annotations, annotations)
	secret.Data = data
	if err := r.Update(ctx, secret); err != nil {
		switch {
		case apierrors.IsConflict(err):
			return nil, errCacheStale
		case apierrors.IsInvalid(err):
			// The Secret was made immutable, or its type was changed:
			// replace it.
			return r.recreateSecret(ctx, gw, existing, ref, secretType, secret)
		}
		return nil, fmt.Errorf("failed to update Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	return secret, nil
}

// resolveExistingSecret handles a Secret that couldn't be created because it
// exists while the cache doesn't show it: either the cache didn't catch up
// with a Secret the GWAdmin just created, or the Secret doesn't carry the
// labels the cache selects on. Only a Secret that isn't the GWAdmin's is a
// conflict. Without reading the Secret, a JSON patch of test operations, which
// changes nothing, checks a proof of ownership, and a merge patch then brings
// the labels back. A Secret is the GWAdmin's when it carries its UID label
// or, in the GWAdmin namespace and in case the labels were lost, when the
// GWAdmin controls it. A Secret in the operator namespace has no owner
// reference, and is never adopted on its name alone: whoever created it
// could hand Kong Operator a certificate authority of their own.
func (r *Reconciler) resolveExistingSecret(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	ref secretRef,
	labels map[string]string,
) error {
	proofs := [][]jsonPatchOp{{{Op: "test", Path: labelPath(consts.GWAdminUIDLabel), Value: string(gw.UID)}}}
	if !ref.private {
		proofs = append(proofs, []jsonPatchOp{
			{Op: "test", Path: "/metadata/ownerReferences/0/uid", Value: string(gw.UID)},
			{Op: "test", Path: "/metadata/ownerReferences/0/controller", Value: true},
		})
	}
	owned := false
	for _, proof := range proofs {
		if owned {
			break
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return fmt.Errorf("failed to build the patch of Secret %s/%s: %w", ref.namespace, ref.name, err)
		}
		switch err := r.Patch(ctx, &corev1.Secret{Namespace: ref.namespace, Name: ref.name}, client.RawPatch(types.JSONPatchType, raw)); {
		case err == nil:
			owned = true
		case apierrors.IsNotFound(err):
			return errCacheStale
		case !apierrors.IsInvalid(err):
			return fmt.Errorf("failed to check the owner of Secret %s/%s: %w", ref.namespace, ref.name, err)
		}
		// Invalid: the test failed, try the next proof.
	}
	if !owned {
		return r.secretConflict(gw, ref, nil)
	}
	raw, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": labels}})
	if err != nil {
		return fmt.Errorf("failed to build the patch of Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	if err := r.Patch(ctx, &corev1.Secret{Namespace: ref.namespace, Name: ref.name}, client.RawPatch(types.MergePatchType, raw)); err != nil &&
		!apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to label Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	// The GWAdmin's own Secret: the cache sees it once labelled.
	return errCacheStale
}

// jsonPatchOp is an operation of a JSON patch (RFC 6902).
type jsonPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// labelPath returns the JSON pointer (RFC 6901) to the label key.
func labelPath(key string) string {
	return "/metadata/labels/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
}

// recreateSecret replaces the existing Secret, which can't be updated, with
// desired.
func (r *Reconciler) recreateSecret(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	existing *corev1.Secret,
	ref secretRef,
	secretType corev1.SecretType,
	desired *corev1.Secret,
) (*corev1.Secret, error) {
	if err := r.Delete(ctx, existing, client.Preconditions{UID: &existing.UID}); err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			return nil, errCacheStale
		}
		return nil, fmt.Errorf("failed to delete Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	secret := &corev1.Secret{
		Namespace: ref.namespace, Name: ref.name,
		Labels: desired.Labels, Annotations: desired.Annotations,
		Type: secretType,
		Data: desired.Data,
	}
	if !ref.private {
		k8sutils.SetOwnerForObject(secret, gw)
	}
	if err := r.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, errCacheStale
		}
		return nil, fmt.Errorf("failed to create Secret %s/%s: %w", ref.namespace, ref.name, err)
	}
	return secret, nil
}
