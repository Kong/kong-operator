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
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// onPremRegion is the region that makes keg take KONG_KONNECT_DOMAIN as the
// host, and port, of its control plane, instead of composing a Konnect host
// from the region and the domain. keg only recognizes it in lowercase.
const onPremRegion = "testing"

// onPremEventGatewayKind wires the OnPremEventGateway control plane kind into
// the shared generic DataPlane reconciler. The data plane pulls its
// configuration from the gw-admin of the GWAdmin of the gateway, and
// registers its client certificate on it through an
// EventGatewayDataPlaneCertificate the OnPremEventGateway controller pushes.
// Dependencies is set per reconciler: it needs the operator namespace.
var onPremEventGatewayKind = shareddataplane.ControlPlaneKindConfig{
	Kind:                      "OnPremEventGateway",
	GVK:                       metav1.GroupVersionKind(eventgatewayv1alpha1.SchemeGroupVersion.WithKind("OnPremEventGateway")),
	NewObject:                 func() shareddataplane.ControlPlaneObject { return &eventgatewayv1alpha1.OnPremEventGateway{} },
	ControlPlaneRefIndexField: index.IndexFieldKegDataPlaneOnOnPremEventGateway,
	Conditions: shareddataplane.ControlPlaneConditions{
		ResolvedType:       string(eventgatewayv1alpha1.OnPremEventGatewayResolvedType),
		ResolvedReason:     string(eventgatewayv1alpha1.OnPremEventGatewayResolvedReason),
		ResolvedMessage:    eventgatewayv1alpha1.OnPremEventGatewayResolvedMessage,
		NotFoundReason:     string(eventgatewayv1alpha1.OnPremEventGatewayNotFoundReason),
		NotFoundMessage:    eventgatewayv1alpha1.OnPremEventGatewayNotFoundMessage,
		NotPermittedReason: string(eventgatewayv1alpha1.OnPremEventGatewayRefNotPermittedReason),
		NotReadyReason:     string(eventgatewayv1alpha1.KegDataPlaneGWAdminNotReadyReason),
	},
	CertificateRegistration: &shareddataplane.CertificateRegistrationConditions{
		Type:                     string(eventgatewayv1alpha1.GWAdminCertificateRegisteredType),
		RegisteredReason:         string(eventgatewayv1alpha1.GWAdminCertificateRegisteredReason),
		RegistrationFailedReason: string(eventgatewayv1alpha1.GWAdminCertificateRegistrationFailedReason),
		NotProgrammedReason:      string(eventgatewayv1alpha1.GWAdminCertificateNotProgrammedReason),
		Target:                   "every gw-admin pod",
		// Only the OnPremEventGateway controller programs it: without it, the
		// certificate never gets a Programmed condition, and the data plane
		// waits forever.
		NotProgrammedHint:         " by the OnPremEventGateway controller (--enable-controller-onpremeventgateway)",
		ReportProgrammedCondition: true,
		CreatedEventReason:        "GWAdminCertificateCreated",
		CreatedEventAction:        "CreateGWAdminCertificate",
		UpdatedEventReason:        "GWAdminCertificateUpdated",
		UpdatedEventAction:        "UpdateGWAdminCertificate",
	},
}

// OnPremGroupVersionResources returns the resources the OnPremEventGateway
// control plane kind needs: the gateways, the GWAdmins serving them, and the
// KongReferenceGrants allowing a gateway to use a GWAdmin in another
// namespace. Unlike those of the Konnect kind, they are optional: without
// them, the kind is disabled.
func OnPremGroupVersionResources() []schema.GroupVersionResource {
	return gwadminref.GroupVersionResources()
}

// OnPremAvailable reports whether the cluster serves every resource the
// OnPremEventGateway control plane kind needs.
func OnPremAvailable(crdExists func(schema.GroupVersionResource) (bool, error)) (bool, error) {
	for _, gvr := range OnPremGroupVersionResources() {
		ok, err := crdExists(gvr)
		if err != nil {
			return false, fmt.Errorf("failed to check existence of CRD %s: %w", gvr, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// onPremDisabledMessage is the message a data plane of an OnPremEventGateway
// is reported with when the kind is disabled.
const onPremDisabledMessage = "OnPremEventGateway control planes are not supported: the onpremeventgateways, gwadmins or kongreferencegrants CRD " +
	"was not installed when Kong Operator started; install them and restart it"

// onPremDependencies is what a data plane of an OnPremEventGateway needs from
// the GWAdmin of the gateway.
type onPremDependencies struct {
	gateway *eventgatewayv1alpha1.OnPremEventGateway
	gwAdmin *operatorv1alpha1.GWAdmin
	// host is the host and port of the cp endpoint of the GWAdmin.
	host string
	// serverBundle is the Secret, in the namespace of the GWAdmin, holding in
	// ca.crt the certificate authorities the data plane verifies the cp
	// listener with.
	serverBundle *corev1.Secret
	// clientCA is the Secret, in the operator namespace, holding the cp client
	// CA the client certificate of the data plane is issued from.
	clientCA *corev1.Secret
	// clientCACert is the certificate of clientCA.
	clientCACert *x509.Certificate
	// manualCertIssuer is the cp client CA that issued the Manual client
	// certificate of the data plane, set by checkManualOnPremCertificate. It
	// is nil for an Automatic one, always issued from clientCACert.
	manualCertIssuer *x509.Certificate
	// manualCertChain is the chain that verified that certificate, from the
	// certificate to manualCertIssuer: what gw-admin needs valid for it.
	manualCertChain []*x509.Certificate
}

// onPremDependenciesFunc returns the Dependencies of the OnPremEventGateway
// kind: the GWAdmin of the gateway, its cp endpoint, its cp server CA bundle,
// and its cp client CA, which the GWAdmin controller publishes in the
// operator namespace once the gw-admin pods trust it.
func onPremDependenciesFunc(operatorNamespace, secretLabelSelector string) func(context.Context, client.Client, shareddataplane.ControlPlaneObject) (any, error) {
	return func(ctx context.Context, cl client.Client, cp shareddataplane.ControlPlaneObject) (any, error) {
		gateway, ok := cp.(*eventgatewayv1alpha1.OnPremEventGateway)
		if !ok {
			return nil, fmt.Errorf("unexpected control plane %T", cp)
		}
		// No new certificate is registered on a gateway being deleted, and
		// the data plane is released from its GWAdmin (see
		// releaseDeploymentFromGWAdmin).
		if !gateway.DeletionTimestamp.IsZero() {
			return nil, &shareddataplane.DependenciesNotReadyError{
				Message:          fmt.Sprintf("The OnPremEventGateway %s/%s is being deleted", gateway.Namespace, gateway.Name),
				ControlPlaneGone: true,
			}
		}

		key := gateway.GWAdminKey()
		reason, err := gwadminref.NotAllowed(ctx, cl, gateway)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return nil, onPremNotReady("The OnPremEventGateway may not use its GWAdmin: %s", reason)
		}
		var gw operatorv1alpha1.GWAdmin
		if err := cl.Get(ctx, key, &gw); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, onPremNotReady("The GWAdmin %s of the OnPremEventGateway does not exist", key)
			}
			return nil, fmt.Errorf("failed to get the GWAdmin %s: %w", key, err)
		}
		if !gw.DeletionTimestamp.IsZero() {
			return nil, onPremNotReady("The GWAdmin %s of the OnPremEventGateway is being deleted", key)
		}
		if gw.Status.CP == nil || gw.Status.CP.ServerURL == "" || gw.Status.TLS == nil || gw.Status.TLS.CPServerCABundleSecretName == "" {
			return nil, onPremNotReady("The GWAdmin %s hasn't published its cp endpoint yet", key)
		}
		host, err := hostAndPort(gw.Status.CP.ServerURL)
		if err != nil {
			return nil, onPremNotReady("The cp endpoint of the GWAdmin %s is invalid: %v", key, err)
		}

		bundle, err := readCPServerBundle(ctx, cl, &gw, secretLabelSelector)
		if err != nil {
			return nil, err
		}
		ca, caCert, err := readCPClientCA(ctx, cl, operatorNamespace, &gw, secretLabelSelector)
		if err != nil {
			return nil, err
		}

		return &onPremDependencies{
			gateway:      gateway,
			gwAdmin:      &gw,
			host:         host,
			serverBundle: bundle,
			clientCA:     ca,
			clientCACert: caCert,
		}, nil
	}
}

// onPremNotReady returns the error the dependencies of an OnPremEventGateway
// are reported not ready with.
func onPremNotReady(format string, args ...any) error {
	return &shareddataplane.DependenciesNotReadyError{Message: fmt.Sprintf(format, args...)}
}

// readCPServerBundle reads the cp server CA bundle Secret the GWAdmin
// published in its namespace, which must hold ca.crt.
func readCPServerBundle(ctx context.Context, cl client.Client, gw *operatorv1alpha1.GWAdmin, secretLabelSelector string) (*corev1.Secret, error) {
	key := client.ObjectKey{Namespace: gw.Namespace, Name: gw.Status.TLS.CPServerCABundleSecretName}
	bundle, err := gwadminref.GetSecret(ctx, cl, key, "cp server CA bundle", nil, secretLabelSelector)
	if err != nil {
		return nil, asDependenciesNotReady(err)
	}
	if len(bundle.Data[consts.CACRT]) == 0 {
		return nil, onPremNotReady("The cp server CA bundle Secret %s of the GWAdmin holds no %s", key, consts.CACRT)
	}
	return bundle, nil
}

// readCPClientCA reads the cp client CA Secret the GWAdmin published in the
// operator namespace, and its certificate.
func readCPClientCA(ctx context.Context, cl client.Client, operatorNamespace string, gw *operatorv1alpha1.GWAdmin, secretLabelSelector string) (*corev1.Secret, *x509.Certificate, error) {
	key := client.ObjectKey{Namespace: operatorNamespace, Name: gwadminref.CPClientCASecretName(gw)}
	ca, err := gwadminref.GetSecret(ctx, cl, key, "cp client CA", gw, secretLabelSelector)
	if err != nil {
		return nil, nil, asDependenciesNotReady(err)
	}
	certs := pki.ParseCertificates(ca.Data[corev1.TLSCertKey])
	if len(certs) == 0 || len(ca.Data[corev1.TLSPrivateKeyKey]) == 0 {
		return nil, nil, onPremNotReady("The cp client CA Secret %s of the GWAdmin is invalid", key)
	}
	return ca, certs[0], nil
}

// asDependenciesNotReady returns the error of a lookup in the GWAdmin,
// telling the dependencies of the data plane aren't ready when the lookup
// does.
func asDependenciesNotReady(err error) error {
	if notReady, ok := errors.AsType[gwadminref.NotReadyError](err); ok {
		return &shareddataplane.DependenciesNotReadyError{Message: notReady.Message}
	}
	return err
}

// hostAndPort returns the host and port of an https URL, with the default
// port when the URL names none.
func hostAndPort(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not an https URL with a host", serverURL)
	}
	port := u.Port()
	if port == "" {
		port = strconv.Itoa(consts.DefaultHTTPSPort)
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

// onPremDependenciesOf returns the dependencies of the resolved control plane
// when it is an OnPremEventGateway, or nil.
func onPremDependenciesOf(cp shareddataplane.ResolvedControlPlane) *onPremDependencies {
	deps, _ := cp.Dependencies.(*onPremDependencies)
	return deps
}

const (
	// secretCopyLabel labels a Secret Kong Operator copied for a data plane of
	// an OnPremEventGateway, with what it is a copy of.
	secretCopyLabel = consts.OperatorLabelPrefix + "secret-copy"
	// secretCopyManualCertificate is the value of secretCopyLabel on the copy
	// of a Manual client certificate (see ensureManualCertificateCopy).
	secretCopyManualCertificate = "manual-certificate"
	// secretCopyServerBundle is the value of secretCopyLabel on the copy of
	// the cp server CA bundle of a GWAdmin (see ensureServerBundleCopy).
	secretCopyServerBundle = "cp-server-ca-bundle" //nolint:gosec
	// secretCopiedFromAnnotation records on such a copy the namespace and name
	// of the Secret it was copied from.
	secretCopiedFromAnnotation = consts.OperatorLabelPrefix + "copied-from"
)

// onPremCertificateIssuedAtAnnotation records on the client certificate
// Secret of a data plane of an OnPremEventGateway when the certificate was
// issued: its NotBefore is backdated, so it can't tell.
const onPremCertificateIssuedAtAnnotation = consts.OperatorAnnotationPrefix + "keg-dp-cert-issued-at"

const (
	// minOnPremCertificateTTL is the shortest validity a client certificate
	// of a data plane of an OnPremEventGateway is issued with: a shorter one
	// would be renewed before the data plane rolled out onto it.
	minOnPremCertificateTTL = time.Hour
	// minOnPremCertificateValidity is the shortest validity worth issuing a
	// certificate with, e.g. when the cp client CA expires soon.
	minOnPremCertificateValidity = 10 * time.Minute
)

// onPremCertificate is a client certificate Secret of a data plane of an
// OnPremEventGateway.
type onPremCertificate struct {
	secret   *corev1.Secret
	cert     *x509.Certificate
	issuedAt time.Time
}

// parseOnPremCertificate returns the certificate of the Secret and when it
// was issued: as recorded on the Secret, or else its NotBefore without the
// backdate of the signer.
func parseOnPremCertificate(secret *corev1.Secret) (onPremCertificate, error) {
	cert := pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if cert == nil {
		return onPremCertificate{}, errors.New("no certificate forming a key pair with its key")
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, secret.Annotations[onPremCertificateIssuedAtAnnotation])
	if err != nil {
		issuedAt = cert.NotBefore.Add(pki.ClockSkew)
	}
	if issuedAt.Before(cert.NotBefore) {
		issuedAt = cert.NotBefore
	}
	if issuedAt.After(cert.NotAfter) {
		issuedAt = cert.NotAfter
	}
	return onPremCertificate{secret: secret, cert: cert, issuedAt: issuedAt}, nil
}

// renewalTime returns when the certificate is due for renewal: once two
// thirds of its validity since it was issued have elapsed, like those of the
// GWAdmin controller.
func (c onPremCertificate) renewalTime() time.Time {
	return pki.RenewalTime(c.issuedAt, c.cert.NotAfter)
}

// onPremCertificateLabels returns the labels of the client certificate
// Secrets of a data plane of an OnPremEventGateway.
func (r *Reconciler) onPremCertificateLabels(egdp *eventgatewayv1alpha1.KegDataPlane) client.MatchingLabels {
	labels := k8sresources.GetManagedLabelForOwner(egdp)
	labels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
	labels[consts.SecretKEGDataPlaneCertificateLabel] = "true"
	if r.SecretLabelSelector != "" {
		labels[r.SecretLabelSelector] = "true"
	}
	return labels
}

// listOnPremCertificates lists the client certificate Secrets of a data
// plane of an OnPremEventGateway: the current one, and those it replaced,
// kept until the Deployment rolled out onto it.
func (r *Reconciler) listOnPremCertificates(ctx context.Context, cl client.Reader, egdp *eventgatewayv1alpha1.KegDataPlane) ([]corev1.Secret, error) {
	owned, err := k8sutils.ListSecretsForOwner(ctx, cl, egdp.GetUID(), client.InNamespace(egdp.Namespace), r.onPremCertificateLabels(egdp))
	if err != nil {
		return nil, fmt.Errorf("failed to list the mTLS certificate Secrets of %s/%s: %w", egdp.Namespace, egdp.Name, err)
	}
	return owned, nil
}

// issuedOnPremCertificates returns the certificates of the Secrets that ca
// issued for the subject, but those being deleted.
func issuedOnPremCertificates(owned []corev1.Secret, subject string, ca *x509.Certificate) []onPremCertificate {
	var issued []onPremCertificate
	for i := range owned {
		c, err := parseOnPremCertificate(&owned[i])
		// CheckSignatureFrom ignores names: a CA replaced by one with the same
		// key, under another subject, would pass, while a verifier refuses it.
		if err != nil || !issuedForSubject(c.cert, subject) || !bytes.Equal(c.cert.RawIssuer, ca.RawSubject) ||
			c.cert.CheckSignatureFrom(ca) != nil || !owned[i].DeletionTimestamp.IsZero() {
			continue
		}
		issued = append(issued, c)
	}
	return issued
}

// issuedAfter reports whether c was issued after other, the name telling
// apart those issued at the same time.
func (c onPremCertificate) issuedAfter(other onPremCertificate) bool {
	return c.issuedAt.After(other.issuedAt) ||
		(c.issuedAt.Equal(other.issuedAt) && c.secret.Name > other.secret.Name)
}

// currentOnPremCertificate returns the most recently issued certificate the
// cp client CA issued for the data plane, or false when there is none.
func currentOnPremCertificate(owned []corev1.Secret, subject string, ca *x509.Certificate) (onPremCertificate, bool) {
	var current onPremCertificate
	found := false
	for _, c := range issuedOnPremCertificates(owned, subject, ca) {
		if !found || c.issuedAfter(current) {
			current, found = c, true
		}
	}
	return current, found
}

// onPremCertificateSubject returns the subject of the client certificate of a
// data plane of an OnPremEventGateway: its DNS name, and its common name once
// truncated (see pki.TruncateCommonName), as the subject can be longer than
// a common name may be.
func onPremCertificateSubject(egdp *eventgatewayv1alpha1.KegDataPlane) string {
	return fmt.Sprintf("%s.%s", egdp.Name, egdp.Namespace)
}

// issuedForSubject reports whether cert was issued for the subject, by
// createOnPremCertificate: its DNS name is the subject, and its common name
// the subject truncated.
func issuedForSubject(cert *x509.Certificate, subject string) bool {
	return cert.Subject.CommonName == pki.TruncateCommonName(subject) && slices.Equal(cert.DNSNames, []string{subject})
}

// referencedOnPremCertificates returns the names of the client certificate
// Secrets an EventGatewayDataPlaneCertificate of the data plane references.
func referencedOnPremCertificates(ctx context.Context, cl client.Client, egdp *eventgatewayv1alpha1.KegDataPlane) (map[string]bool, error) {
	var certs configurationv1alpha1.EventGatewayDataPlaneCertificateList
	if err := cl.List(ctx, &certs, client.InNamespace(egdp.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list the EventGatewayDataPlaneCertificates of %s/%s: %w", egdp.Namespace, egdp.Name, err)
	}
	referenced := map[string]bool{}
	for i := range certs.Items {
		c := &certs.Items[i]
		if ref := c.Spec.APISpec.Certificate.SecretRef; ref != nil && metav1.IsControlledBy(c, egdp) {
			referenced[ref.Name] = true
		}
	}
	return referenced, nil
}

// reduceOnPremCertificates deletes the duplicates of the current client
// certificate of a data plane of an OnPremEventGateway, which reconciles
// issue when the cache doesn't hold the certificate an earlier one just
// issued yet: the certificates the cp client CA issued for the data plane
// that aren't due for renewal. Those an EventGatewayDataPlaneCertificate of
// the data plane references are kept, or else the most recently issued one.
// The certificates due for renewal, and those of a previous CA, are left to
// cleanupStaleOnPremCertificates: the Deployment may still use them. It
// returns the Secrets left.
func reduceOnPremCertificates(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	owned []corev1.Secret,
	ca *x509.Certificate,
	now time.Time,
) ([]corev1.Secret, error) {
	var fresh []onPremCertificate
	for _, c := range issuedOnPremCertificates(owned, onPremCertificateSubject(egdp), ca) {
		if now.Before(c.renewalTime()) {
			fresh = append(fresh, c)
		}
	}
	if len(fresh) < 2 {
		return owned, nil
	}
	referenced, err := referencedOnPremCertificates(ctx, cl, egdp)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	newest := fresh[0]
	for _, c := range fresh {
		if referenced[c.secret.Name] {
			keep[c.secret.Name] = true
		}
		if c.issuedAfter(newest) {
			newest = c
		}
	}
	if len(keep) == 0 {
		keep[newest.secret.Name] = true
	}
	deleted := map[string]bool{}
	for _, c := range fresh {
		if keep[c.secret.Name] {
			continue
		}
		if err := cl.Delete(ctx, c.secret); client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("failed to delete the duplicate mTLS certificate Secret %s/%s: %w", c.secret.Namespace, c.secret.Name, err)
		}
		deleted[c.secret.Name] = true
	}
	return slices.DeleteFunc(owned, func(s corev1.Secret) bool { return deleted[s.Name] }), nil
}

// freshOnPremCertificateValidity returns how long a certificate issued now
// from ca would be valid: its validity counts from the backdated NotBefore, and
// is capped at the expiry of ca.
func freshOnPremCertificateValidity(ca *x509.Certificate, ttl time.Duration, now time.Time) time.Duration {
	return min(ttl-pki.ClockSkew, ca.NotAfter.Sub(now))
}

// renewalDeferred reports whether the expiry of ca nears so much that renewing
// the certificate now from it would not at least double its remaining validity:
// renewing it would only roll the pods ever more often. It is renewed once the
// GWAdmin renews ca. A certificate whose new validity ca doesn't cap, e.g. once
// the configured one is lowered, is renewed when due.
func renewalDeferred(current onPremCertificate, ca *x509.Certificate, ttl time.Duration, now time.Time) bool {
	if ca.NotAfter.Sub(now) >= ttl-pki.ClockSkew {
		return false
	}
	fresh := freshOnPremCertificateValidity(ca, ttl, now)
	return fresh < minOnPremCertificateValidity || !pki.RenewalExtends(fresh, current.cert.NotAfter.Sub(now))
}

// ensureOnPremCertificate provisions the client certificate of a data plane
// of an OnPremEventGateway, issued from the cp client CA of its GWAdmin.
func (r *Reconciler) ensureOnPremCertificate(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
) (op.Result, *corev1.Secret, error) {
	return r.ensureOnPremCertificateAt(ctx, cl, egdp, deps, time.Now())
}

// ensureOnPremCertificateAt provisions the client certificate of a data
// plane of an OnPremEventGateway at now. Once the certificate is due for
// renewal, or the published CA didn't issue it, e.g. after a rotation of the
// CA, a new one is issued in a new Secret, alongside the previous one: the
// Deployment rolls onto it once it is registered on gw-admin, and the
// previous one is removed after the rollout (see cleanupStaleCertificates).
func (r *Reconciler) ensureOnPremCertificateAt(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
	now time.Time,
) (op.Result, *corev1.Secret, error) {
	owned, current, found, err := r.currentOnPremCertificateOf(ctx, cl, egdp, deps.clientCACert, now)
	if err != nil {
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, err.Error())
		return op.Noop, nil, err
	}
	reissue := ""
	switch {
	case !found && len(owned) > 0:
		reissue = "the cp client CA of the GWAdmin didn't issue the current one"
	case found && !now.Before(current.renewalTime()):
		reissue = "the current one is due for renewal"
	case found:
		setCertificateProvisioned(egdp, metav1.ConditionTrue, eventgatewayv1alpha1.CertificateProvisionedReason, "mTLS certificate Secret provisioned")
		return op.Noop, current.secret, nil
	}

	ttl := onPremCertificateTTL(r.CertTTL, r.GWAdminCertTTL)
	if deferred, secret, err := deferOnPremCertificate(egdp, current, found, deps.clientCACert, ttl, now); deferred {
		return op.Noop, secret, err
	}
	return r.issueOnPremCertificate(ctx, cl, egdp, deps, ttl, now, reissue)
}

// setCertificateProvisioned sets the CertificateProvisioned condition of the
// data plane.
func setCertificateProvisioned(egdp *eventgatewayv1alpha1.KegDataPlane, status metav1.ConditionStatus, reason kcfgconsts.ConditionReason, message string) {
	apimeta.SetStatusCondition(&egdp.Status.Conditions, k8sutils.NewConditionWithGeneration(
		eventgatewayv1alpha1.CertificateProvisionedType, status, reason, message, egdp.GetGeneration(),
	))
}

// currentOnPremCertificateOf lists the client certificate Secrets of a data
// plane of an OnPremEventGateway, deletes the duplicates of the current one
// (see reduceOnPremCertificates), and returns the Secrets left and the
// current certificate, or false when ca issued none.
func (r *Reconciler) currentOnPremCertificateOf(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	ca *x509.Certificate,
	now time.Time,
) ([]corev1.Secret, onPremCertificate, bool, error) {
	owned, err := r.listOnPremCertificates(ctx, cl, egdp)
	if err != nil {
		return nil, onPremCertificate{}, false, err
	}
	if owned, err = reduceOnPremCertificates(ctx, cl, egdp, owned, ca, now); err != nil {
		return nil, onPremCertificate{}, false, err
	}
	current, found := currentOnPremCertificate(owned, onPremCertificateSubject(egdp), ca)
	return owned, current, found, nil
}

// deferOnPremCertificate reports whether issuing the client certificate of a
// data plane of an OnPremEventGateway from ca at now is deferred: ca expires
// too soon to issue one from it, or, when there is a current certificate
// (found), renewing it wouldn't extend it (see renewalDeferred). It then sets
// the CertificateProvisioned condition, and returns the certificate Secret
// to keep using, if any, and the error to return.
func deferOnPremCertificate(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	current onPremCertificate,
	found bool,
	ca *x509.Certificate,
	ttl time.Duration,
	now time.Time,
) (bool, *corev1.Secret, error) {
	deferred := freshOnPremCertificateValidity(ca, ttl, now) < minOnPremCertificateValidity
	if found {
		deferred = renewalDeferred(current, ca, ttl, now)
	}
	if !deferred {
		return false, nil, nil
	}
	caExpiry := ca.NotAfter.UTC().Format(time.RFC3339)
	switch {
	case !found:
		// The GWAdmin renews its CA long before it expires, and the data
		// planes are reconciled when it does.
		msg := fmt.Sprintf("The cp client CA of the GWAdmin expires at %s, too soon to issue the mTLS certificate from it", caExpiry)
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, msg)
		return true, nil, &shareddataplane.RequeueAfterError{After: maxDeferredRenewalRequeue, Err: errors.New(msg)}
	case !now.Before(current.cert.NotAfter):
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, fmt.Sprintf(
			"The mTLS certificate expired, and the cp client CA of the GWAdmin expires at %s, too soon to issue a new one from it", caExpiry))
	default:
		setCertificateProvisioned(egdp, metav1.ConditionTrue, eventgatewayv1alpha1.CertificateProvisionedReason, fmt.Sprintf(
			"mTLS certificate Secret provisioned; its renewal waits for the GWAdmin to renew its cp client CA, which expires at %s", caExpiry))
	}
	return true, current.secret, nil
}

// issueOnPremCertificate issues the client certificate of a data plane of an
// OnPremEventGateway from the cp client CA of its GWAdmin in a new Secret,
// recording when it was issued. reissue, when set, tells why the current one
// is replaced.
func (r *Reconciler) issueOnPremCertificate(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
	ttl time.Duration,
	now time.Time,
	reissue string,
) (op.Result, *corev1.Secret, error) {
	secret, err := r.createOnPremCertificate(ctx, cl, egdp, deps, ttl, now)
	if err != nil {
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason,
			fmt.Sprintf("failed to provision mTLS certificate Secret from the cp client CA of the GWAdmin: %v", err))
		return op.Noop, nil, err
	}
	msg := "mTLS certificate Secret provisioned"
	if reissue != "" {
		msg += ", issued again: " + reissue
	}
	setCertificateProvisioned(egdp, metav1.ConditionTrue, eventgatewayv1alpha1.CertificateProvisionedReason, msg)
	return op.Created, secret, nil
}

// createOnPremCertificate creates a new Secret holding a client certificate
// issued from the cp client CA of the GWAdmin of the data plane, signed by the
// first certificate of its tls.crt, the one gw-admin trusts: a Manual cp client
// CA may hold a chain.
func (r *Reconciler) createOnPremCertificate(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
	ttl time.Duration,
	now time.Time,
) (*corev1.Secret, error) {
	subject := onPremCertificateSubject(egdp)
	certPEM, keyPEM, err := pki.IssueCertificate(deps.clientCA, pki.CertRequest{
		CommonName: pki.TruncateCommonName(subject),
		DNSNames:   []string{subject},
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		TTL:        ttl,
		Now:        now,
	})
	if err != nil {
		return nil, err
	}
	certs := pki.ParseCertificates(certPEM)
	if len(certs) == 0 {
		return nil, errors.New("the certificate issued can't be parsed")
	}
	secret := k8sresources.GenerateNewTLSSecret(egdp, func(s *corev1.Secret) {
		maps.Copy(s.Labels, r.onPremCertificateLabels(egdp))
		s.Annotations = map[string]string{
			onPremCertificateIssuedAtAnnotation: now.UTC().Format(time.RFC3339Nano),
			consts.CertExpiresAtAnnotation:      certs[0].NotAfter.UTC().Format(time.RFC3339),
		}
	})
	secret.Data = map[string][]byte{
		consts.CACRT:            pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: deps.clientCACert.Raw}),
		corev1.TLSCertKey:       certPEM,
		corev1.TLSPrivateKeyKey: keyPEM,
	}
	if err := cl.Create(ctx, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// onPremCertificateTTL returns the validity of the client certificates of
// the data planes of an OnPremEventGateway: the configured one, capped to the
// validity of the certificates the GWAdmin controller issues
// (--gwadmin-cert-ttl), so that they are renewed as often, and at least
// minOnPremCertificateTTL.
func onPremCertificateTTL(ttl, gwAdminCertTTL time.Duration) time.Duration {
	if gwAdminCertTTL <= 0 {
		gwAdminCertTTL = consts.GWAdminLeafTTL
	}
	if ttl <= 0 || ttl > gwAdminCertTTL {
		ttl = gwAdminCertTTL
	}
	return max(ttl, minOnPremCertificateTTL)
}

// trustedClientCAs returns the cp client CAs gw-admin verifies the client
// certificates of the data planes with at now: the published one, and the
// unexpired ones published before it, which the gw-admin pods keep trusting
// until every data plane runs a certificate from the published one.
func (d *onPremDependencies) trustedClientCAs(now time.Time) []*x509.Certificate {
	trusted := []*x509.Certificate{d.clientCACert}
	for _, ca := range pki.ParseCertificates(d.clientCA.Data[gwadminref.PreviousCAKey]) {
		if ca.IsCA && now.Before(ca.NotAfter) {
			trusted = append(trusted, ca)
		}
	}
	return trusted
}

// manualCertificateIssuer returns the cp client CA of trusted that issued the
// certificate of the Secret, directly or through the intermediates its
// tls.crt holds, or nil when none did. The certificate is checked at now, or
// at its NotBefore when it is in the future: the clock skew tolerated for a
// Manual certificate (see shareddataplane.GetManualCertificateSecret).
func manualCertificateIssuer(secret *corev1.Secret, trusted []*x509.Certificate, now time.Time) *x509.Certificate {
	chain := verifiedManualCertificateChain(secret, trusted, now)
	if len(chain) == 0 {
		return nil
	}
	return chain[len(chain)-1]
}

// verifiedManualCertificateChain returns the chain from the certificate of the
// Secret to the cp client CA of trusted that issued it, or nil when none did (see
// manualCertificateIssuer).
func verifiedManualCertificateChain(secret *corev1.Secret, trusted []*x509.Certificate, now time.Time) []*x509.Certificate {
	leaf := pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if leaf == nil {
		return nil
	}
	intermediates := x509.NewCertPool()
	for _, c := range pki.ParseCertificates(secret.Data[corev1.TLSCertKey]) {
		if !c.Equal(leaf) {
			intermediates.AddCert(c)
		}
	}
	at := now
	if at.Before(leaf.NotBefore) {
		at = leaf.NotBefore
	}
	for _, ca := range trusted {
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		chains, err := leaf.Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			CurrentTime:   at,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		if err == nil && len(chains) > 0 {
			return chains[0]
		}
	}
	return nil
}

// checkManualOnPremCertificate checks a cp client CA gw-admin trusts issued
// the Manual client certificate of a data plane of an OnPremEventGateway,
// and records it in deps, so that its rotation waits for the data plane (see
// customizeDeployment). It returns the Secret, or nil when gw-admin would
// reject the certificate: the Deployment isn't rolled onto it then. A change
// of the Secret, or of the cp client CA the GWAdmin publishes, reconciles the
// data plane again.
func checkManualOnPremCertificate(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
	secret *corev1.Secret,
	now time.Time,
) *corev1.Secret {
	// Its key would be mounted into the pods of the data plane.
	if leaf := pki.LeafCertificate(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey]); leaf != nil && leaf.IsCA {
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.CertificateSecretInvalidReason, fmt.Sprintf(
			"The certificate of the Secret %s is a certificate authority: a data plane needs a client certificate, not a certificate authority's key", secret.Name))
		return nil
	}
	chain := verifiedManualCertificateChain(secret, deps.trustedClientCAs(now), now)
	if len(chain) == 0 {
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.CertificateSecretInvalidReason, fmt.Sprintf(
			"The certificate of the Secret %s isn't issued, for client authentication, from the cp client CA of the GWAdmin %s/%s, "+
				"nor from one it still trusts: gw-admin would reject it",
			secret.Name, deps.gwAdmin.Namespace, deps.gwAdmin.Name))
		return nil
	}
	deps.manualCertIssuer = chain[len(chain)-1]
	deps.manualCertChain = chain
	return secret
}

const (
	// minDeferredRenewalRequeue and maxDeferredRenewalRequeue bound the delay
	// until a certificate whose renewal is deferred is checked again: the
	// renewal of the CA, which allows it, reconciles the data plane anyway.
	minDeferredRenewalRequeue = time.Minute
	maxDeferredRenewalRequeue = time.Hour
)

// onPremCertificateRequeue returns when the client certificate of a data
// plane of an OnPremEventGateway must be checked again for its renewal.
func (r *Reconciler) onPremCertificateRequeue(egdp *eventgatewayv1alpha1.KegDataPlane, cp shareddataplane.ResolvedControlPlane, secret *corev1.Secret, now time.Time) time.Duration {
	deps := onPremDependenciesOf(cp)
	if deps == nil || secret == nil {
		return 0
	}
	if isManualProvisioning(egdp) {
		return manualCertificateRequeue(deps, now)
	}
	c, err := parseOnPremCertificate(secret)
	if err != nil {
		return 0
	}
	if due := c.renewalTime(); now.Before(due) {
		return min(due.Sub(now)+time.Second, pki.MaxRenewalRequeue)
	}
	// Its renewal is deferred: see ensureOnPremCertificateAt.
	return min(max(c.cert.NotAfter.Sub(now)/2, minDeferredRenewalRequeue), maxDeferredRenewalRequeue)
}

// manualCertificateRequeue returns when a Manual client certificate must be
// checked again: the user renews it, so the next time one of the certificates it
// depends on is reported as about to expire, or expires, or 0 when all did.
func manualCertificateRequeue(deps *onPremDependencies, now time.Time) time.Duration {
	next := time.Duration(0)
	for _, cert := range deps.manualCertChain {
		for _, at := range []time.Time{pki.WarningTime(cert), cert.NotAfter.Add(time.Second)} {
			if after := at.Sub(now); after > 0 && (next == 0 || after < next) {
				next = after
			}
		}
	}
	return min(next, pki.MaxRenewalRequeue)
}

// warnManualCertificateExpiring reports a Manual client certificate, or a
// certificate authority it depends on, about to expire, as the GWAdmin controller
// does for the certificates the user provides to it: in the
// CertificateProvisioned condition, and with one CertificateExpiringSoon warning
// event. The condition tells what was reported before a restart of the operator,
// in before, and warnedExpiring since, as it can be overwritten meanwhile, e.g. by
// an outage of the GWAdmin.
func (r *Reconciler) warnManualCertificateExpiring(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
	secretName string,
	before *metav1.Condition,
	now time.Time,
) {
	current := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.CertificateProvisionedType))
	if current == nil {
		return
	}
	warned := map[string]struct{}{}
	v, known := r.warnedExpiring.Load(egdp.GetUID())
	if known {
		warned, _ = v.(map[string]struct{})
	}
	warnings := map[string]struct{}{}
	for i, cert := range deps.manualCertChain {
		if now.Before(pki.WarningTime(cert)) {
			continue
		}
		warning := fmt.Sprintf("client certificate Secret %q expires at %s", secretName, cert.NotAfter.UTC().Format(time.RFC3339))
		if i > 0 {
			warning = fmt.Sprintf("certificate authority %q issuing the client certificate Secret %q expires at %s",
				cert.Subject.CommonName, secretName, cert.NotAfter.UTC().Format(time.RFC3339))
		}
		current.Message += "; " + warning
		warnings[warning] = struct{}{}
		_, reported := warned[warning]
		if r.eventRecorder != nil && !reported && (before == nil || !strings.Contains(before.Message, warning)) {
			r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeWarning, "CertificateExpiringSoon", "ValidateCertificate", "%s", warning)
		}
	}
	if len(warnings) == 0 {
		r.warnedExpiring.Delete(egdp.GetUID())
		return
	}
	if !known {
		// Only a new entry grows warnedExpiring. Failing to drop the old ones
		// doesn't affect this data plane, and it is tried again on the next one.
		if err := r.forgetDeletedDataPlanes(ctx, cl); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "failed to drop the certificate expiry warnings of deleted data planes")
		}
	}
	r.warnedExpiring.Store(egdp.GetUID(), warnings)
}

// forgetDeletedDataPlanes drops from warnedExpiring the data planes that no
// longer exist. They have no finalizer, so no reconcile sees them go. Dropping
// one still in use by mistake only reports its warnings again.
func (r *Reconciler) forgetDeletedDataPlanes(ctx context.Context, cl client.Client) error {
	var dataPlanes eventgatewayv1alpha1.KegDataPlaneList
	if err := cl.List(ctx, &dataPlanes); err != nil {
		return err
	}
	existing := make(map[types.UID]struct{}, len(dataPlanes.Items))
	for i := range dataPlanes.Items {
		existing[dataPlanes.Items[i].GetUID()] = struct{}{}
	}
	r.warnedExpiring.Range(func(uid, _ any) bool {
		if _, ok := existing[uid.(types.UID)]; !ok {
			r.warnedExpiring.Delete(uid)
		}
		return true
	})
	return nil
}

// cleanupStaleOnPremCertificates deletes the client certificate Secrets of a
// data plane of an OnPremEventGateway but the current one, once no
// EventGatewayDataPlaneCertificate of the data plane references them
// anymore: the stale ones are deleted first, and gw-admin keeps trusting a
// certificate until its EventGatewayDataPlaneCertificate is gone. Their
// deletion reconciles the data plane again.
func (r *Reconciler) cleanupStaleOnPremCertificates(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	certChecksum string,
) error {
	owned, err := r.listOnPremCertificates(ctx, cl, egdp)
	if err != nil {
		return err
	}
	referenced, err := referencedOnPremCertificates(ctx, cl, egdp)
	if err != nil {
		return err
	}
	for i := range owned {
		secret := &owned[i]
		if referenced[secret.Name] || shareddataplane.CertificateChecksum(secret) == certChecksum {
			continue
		}
		if err := cl.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete the stale mTLS certificate Secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
		log.Debug(logger, "stale mTLS certificate Secret removed", "name", secret.Name)
	}
	return cleanupStaleManualCertificateCopies(ctx, cl, logger, egdp, referenced, certChecksum)
}

// cleanupStaleManualCertificateCopies deletes the copies of the Manual client
// certificates of a data plane but the current one, once no
// EventGatewayDataPlaneCertificate of the data plane references them anymore
// (see ensureManualCertificateCopy).
func cleanupStaleManualCertificateCopies(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	referenced map[string]bool,
	certChecksum string,
) error {
	var copies corev1.SecretList
	if err := cl.List(ctx, &copies, client.InNamespace(egdp.Namespace), client.MatchingLabels{secretCopyLabel: secretCopyManualCertificate}); err != nil {
		return fmt.Errorf("failed to list the copies of the Manual client certificates of %s/%s: %w", egdp.Namespace, egdp.Name, err)
	}
	current := shareddataplane.CertEntityName(egdp.Name, certChecksum)
	for i := range copies.Items {
		copied := &copies.Items[i]
		if !metav1.IsControlledBy(copied, egdp) || referenced[copied.Name] || copied.Name == current {
			continue
		}
		if err := cl.Delete(ctx, copied); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete the copy %s/%s of a Manual client certificate: %w", copied.Namespace, copied.Name, err)
		}
		log.Debug(logger, "stale copy of a Manual client certificate removed", "name", copied.Name)
	}
	return nil
}

// Paths of the cp server CA bundle in the keg container.
const (
	gwAdminCPServerCAVolumeName = "gw-admin-cp-server-ca"
	gwAdminCPServerCAMountPath  = "/var/gw-admin-cp-server-ca/"
)

// onPremEnvVars returns the environment variables connecting keg to the
// gw-admin of its OnPremEventGateway: keg calls the cp endpoint of the
// GWAdmin directly, verifies it with the cp server CA bundle, and
// authenticates with its client certificate, registered on the gateway.
//
// keg has no setting for the certificate authorities of its control plane
// alone (none under konnect in its bootstrap schema): it trusts those of
// SSL_CERT_FILE and of every file of SSL_CERT_DIR, for every TLS connection.
// SSL_CERT_FILE stays the system trust store of the image, set explicitly so
// that it isn't lost when SSL_CERT_DIR is set, and SSL_CERT_DIR adds the cp
// server CA bundle: keg trusts gw-admin, and the public certificate
// authorities of the backend clusters and schema registries as before.
// Replace it with a control plane specific setting once keg has one.
func onPremEnvVars(deps *onPremDependencies) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: EnvKonnectRegion, Value: onPremRegion},
		{Name: EnvKonnectDomain, Value: deps.host},
		{Name: EnvKonnectGatewayClusterID, Value: string(deps.gateway.UID)},
		{Name: EnvKonnectClientCertPath, Value: KonnectCertMountPath + "tls.crt"},
		{Name: EnvKonnectClientKeyPath, Value: KonnectCertMountPath + "tls.key"},
		{Name: EnvSSLCertFile, Value: SystemTrustStorePath},
		{Name: EnvSSLCertDir, Value: gwAdminCPServerCAMountPath},
	}
}

// onPremVolume returns the volume of the cp server CA bundle, and its mount:
// the Secret of the GWAdmin, or its copy in the namespace of the data plane.
func onPremVolume(egdp *eventgatewayv1alpha1.KegDataPlane, deps *onPremDependencies) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: gwAdminCPServerCAVolumeName,
		Secret: &corev1.SecretVolumeSource{
			SecretName: serverBundleSecretName(egdp, deps),
			Items:      []corev1.KeyToPath{{Key: consts.CACRT, Path: consts.CACRT}},
		},
	}
	mount := corev1.VolumeMount{
		Name:      gwAdminCPServerCAVolumeName,
		MountPath: gwAdminCPServerCAMountPath,
		ReadOnly:  true,
	}
	return volume, mount
}

// customizeDeployment labels the Deployment of a data plane of an
// OnPremEventGateway with its GWAdmin, and records on its Pod template the
// trust material its pods mount, so that the rotations of the GWAdmin wait
// for the data plane to roll out.
func customizeDeployment(_ *eventgatewayv1alpha1.KegDataPlane, cp shareddataplane.ResolvedControlPlane, d *appsv1.Deployment) {
	deps := onPremDependenciesOf(cp)
	if deps == nil {
		return
	}
	labels := maps.Clone(d.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[consts.GWAdminDataPlaneNamespaceLabel] = deps.gwAdmin.Namespace
	labels[consts.GWAdminDataPlaneNameLabel] = deps.gwAdmin.Name
	d.Labels = labels

	annotations := maps.Clone(d.Spec.Template.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[consts.GWAdminDataPlaneServerBundleAnnotation] = gwadminref.DataPlaneServerBundleChecksum(deps.serverBundle)
	// An Automatic client certificate is always issued from the published
	// CA: one from another CA is issued again before the Deployment is
	// built. A Manual one comes from whichever trusted CA issued it, which
	// the GWAdmin then keeps trusting until the user replaces it.
	clientCA := gwadminref.DataPlaneClientCAFingerprint(deps.clientCA.Data[corev1.TLSCertKey])
	if deps.manualCertIssuer != nil {
		clientCA = pki.Fingerprint(deps.manualCertIssuer)
	}
	annotations[consts.GWAdminDataPlaneClientCAAnnotation] = clientCA
	d.Spec.Template.Annotations = annotations
}

// releaseDeploymentFromGWAdmin stops labelling the Deployment of a data plane
// for the GWAdmin of its OnPremEventGateway once the gateway is gone, or
// being deleted: the GWAdmin waits for the data planes it finds by these
// labels before retiring a certificate authority (see customizeDeployment),
// and the pods of a gone gateway never move. Only the labels of the
// Deployment are removed, not its Pod template: the pods aren't rolled. The
// next build of the Deployment labels it again, e.g. once a gateway of the
// same name exists.
func releaseDeploymentFromGWAdmin(ctx context.Context, cl client.Client, egdp *eventgatewayv1alpha1.KegDataPlane) error {
	var d appsv1.Deployment
	if err := cl.Get(ctx, client.ObjectKeyFromObject(egdp), &d); err != nil {
		return client.IgnoreNotFound(err)
	}
	_, labelledNamespace := d.Labels[consts.GWAdminDataPlaneNamespaceLabel]
	_, labelledName := d.Labels[consts.GWAdminDataPlaneNameLabel]
	if !metav1.IsControlledBy(&d, egdp) || (!labelledNamespace && !labelledName) {
		return nil
	}
	old := d.DeepCopy()
	delete(d.Labels, consts.GWAdminDataPlaneNamespaceLabel)
	delete(d.Labels, consts.GWAdminDataPlaneNameLabel)
	if err := cl.Patch(ctx, &d, client.MergeFrom(old)); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to release the Deployment %s from the GWAdmin of its gone OnPremEventGateway: %w", client.ObjectKeyFromObject(&d), err)
	}
	return nil
}

// serverBundleSecretName returns the name of the Secret holding the cp server
// CA bundle in the namespace of the data plane: the Secret of the GWAdmin, or
// a copy when the GWAdmin is in another namespace, since a Pod can't mount a
// Secret of another namespace.
func serverBundleSecretName(egdp *eventgatewayv1alpha1.KegDataPlane, deps *onPremDependencies) string {
	if egdp.Namespace == deps.serverBundle.Namespace {
		return deps.serverBundle.Name
	}
	return serverBundleCopyName(egdp)
}

// serverBundleCopyName returns the name of the copy of the cp server CA
// bundle of the GWAdmin in the namespace of the data plane. When the name of
// the data plane must be truncated, a hash of it is retained to tell apart
// names with a shared prefix.
func serverBundleCopyName(egdp *eventgatewayv1alpha1.KegDataPlane) string {
	const (
		suffix         = "-gw-admin-cp-server-ca"
		nameHashLength = 10
	)
	name := egdp.Name
	if len(name)+len(suffix) <= validation.DNS1123SubdomainMaxLength {
		return name + suffix
	}
	nameHash := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:nameHashLength]
	name = strings.TrimRight(name[:validation.DNS1123SubdomainMaxLength-len(suffix)-1-nameHashLength], ".-")
	return name + "-" + nameHash + suffix
}

// ensureServerBundleCopy copies the cp server CA bundle of the GWAdmin into
// the namespace of the data plane when the GWAdmin is in another namespace,
// and keeps it up to date: the rotations of the GWAdmin change it.
func (r *Reconciler) ensureServerBundleCopy(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	deps *onPremDependencies,
) (op.Result, error) {
	if egdp.Namespace == deps.serverBundle.Namespace {
		return op.Noop, nil
	}
	res, _, err := r.ensureSecretCopy(ctx, cl, egdp, serverBundleCopyName(egdp), "the cp server CA bundle of the GWAdmin", secretCopyServerBundle, "", deps.serverBundle,
		map[string][]byte{consts.CACRT: deps.serverBundle.Data[consts.CACRT]})
	return res, err
}

// ensureManualCertificateCopy copies the Manual client certificate of a data
// plane, with its key, into a Secret of its own, named after their checksum,
// like its EventGatewayDataPlaneCertificate, and returns it. The user, or
// cert-manager, replaces a Manual certificate in place, so the Secret of the user
// can't be used directly:
//   - gw-admin must keep trusting the previous certificate until the pods rolled
//     out onto the new one: the EventGatewayDataPlaneCertificate of the previous
//     one keeps registering it from its copy until it is deleted, once they did
//     (see cleanupStaleOnPremCertificates).
//   - A pod starting at any time, e.g. a scale up or a restart, must get a
//     certificate gw-admin trusts: the Deployment mounts the copy of the current
//     one, and moves to a new copy only once it was checked and is registered. A
//     certificate that isn't valid, or the Secret being deleted, never reaches
//     the pods.
//
// Like the certificates issued from the cp client CA, a copy is immutable: another
// certificate is another copy.
func (r *Reconciler) ensureManualCertificateCopy(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	secret *corev1.Secret,
) (op.Result, *corev1.Secret, error) {
	name := shareddataplane.CertEntityName(egdp.Name, shareddataplane.CertificateChecksum(secret))
	return r.ensureSecretCopy(ctx, cl, egdp, name, "the Manual client certificate", secretCopyManualCertificate, corev1.SecretTypeTLS, secret,
		map[string][]byte{corev1.TLSCertKey: secret.Data[corev1.TLSCertKey], corev1.TLSPrivateKeyKey: secret.Data[corev1.TLSPrivateKeyKey]})
}

// ensureSecretCopy makes the Secret of the given name, in the namespace of the
// data plane and controlled by it, hold the data copied from source, what naming
// what it holds in messages, and returns it. It is labelled as a copy of the given
// kind, and annotated with its source. secretType, when set, is the type it is
// created with. It never takes over a Secret of the same name the data plane
// doesn't control.
func (r *Reconciler) ensureSecretCopy(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	name, what, kind string,
	secretType corev1.SecretType,
	source *corev1.Secret,
	data map[string][]byte,
) (op.Result, *corev1.Secret, error) {
	copied := &corev1.Secret{Namespace: egdp.Namespace, Name: name}
	res, err := controllerutil.CreateOrUpdate(ctx, cl, copied, func() error {
		if copied.ResourceVersion != "" && !metav1.IsControlledBy(copied, egdp) {
			return errSecretCopyNotControlled
		}
		labels := selectorLabelsForKegDataPlane(egdp)
		if r.SecretLabelSelector != "" {
			labels[r.SecretLabelSelector] = "true"
		}
		labels[secretCopyLabel] = kind
		if copied.Labels == nil {
			copied.Labels = map[string]string{}
		}
		maps.Copy(copied.Labels, labels)
		if copied.Annotations == nil {
			copied.Annotations = map[string]string{}
		}
		copied.Annotations[secretCopiedFromAnnotation] = client.ObjectKeyFromObject(source).String()
		copied.Data = data
		if copied.ResourceVersion == "" {
			copied.Type = secretType
		}
		k8sutils.SetOwnerForObject(copied, egdp)
		return nil
	})
	if errors.Is(err, errSecretCopyNotControlled) {
		msg := fmt.Sprintf("The Secret %s/%s, where %s is copied, exists and isn't controlled by the KegDataPlane", copied.Namespace, copied.Name, what)
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, msg)
		return op.Noop, nil, errors.New(msg)
	}
	if apierrors.IsAlreadyExists(err) {
		// The Secret exists, but the cache doesn't hold it: it lacks the
		// label the cache selects the Secrets of the operator with, or it is
		// the copy an earlier reconcile just created, in which case the next
		// reconcile, once the cache holds it, clears the condition.
		// Only a write tells them apart: the cache can't, and it is never read
		// around. It changes nothing, and fails unless the data plane controls it.
		controlled, patchErr := secretControlledBy(ctx, cl, copied, egdp)
		if patchErr != nil {
			return op.Noop, nil, patchErr
		}
		if controlled {
			return op.Noop, nil, &shareddataplane.RequeueAfterError{
				After: secretCopyCacheRequeue,
				Err:   fmt.Errorf("the cache doesn't hold %s/%s, where %s is copied, yet", copied.Namespace, copied.Name, what),
			}
		}
		remedy := ": it must be removed"
		if hint := gwadminref.SecretHint(r.SecretLabelSelector); hint != "" {
			remedy = hint + ", or it must be removed"
		}
		msg := fmt.Sprintf("A Secret named %s/%s, where %s is copied, already exists but isn't visible to the operator%s", copied.Namespace, copied.Name, what, remedy)
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, msg)
		return op.Noop, nil, errors.New(msg)
	}
	if err != nil {
		err = fmt.Errorf("failed to copy %s into %s/%s: %w", what, copied.Namespace, copied.Name, err)
		setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.UnableToProvisionReason, err.Error())
		return op.Noop, nil, err
	}
	switch res {
	case controllerutil.OperationResultCreated:
		return op.Created, copied, nil
	case controllerutil.OperationResultUpdated, controllerutil.OperationResultUpdatedStatus, controllerutil.OperationResultUpdatedStatusOnly:
		return op.Updated, copied, nil
	default:
		return op.Noop, copied, nil
	}
}

// secretCopyCacheRequeue is how soon a copy just created is looked at again when
// the cache doesn't hold it yet.
const secretCopyCacheRequeue = time.Second

// secretControlledBy reports whether the data plane controls the Secret, or the
// Secret is gone meanwhile, with a JSON patch that only tests it.
func secretControlledBy(ctx context.Context, cl client.Client, secret *corev1.Secret, egdp *eventgatewayv1alpha1.KegDataPlane) (bool, error) {
	test, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/ownerReferences/0/uid", "value": string(egdp.GetUID())},
		{"op": "test", "path": "/metadata/ownerReferences/0/controller", "value": true},
	})
	if err != nil {
		return false, fmt.Errorf("failed to build the patch of the Secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
	switch err := cl.Patch(ctx, &corev1.Secret{Namespace: secret.Namespace, Name: secret.Name}, client.RawPatch(types.JSONPatchType, test)); {
	case err == nil, apierrors.IsNotFound(err):
		return true, nil
	case apierrors.IsInvalid(err):
		return false, nil
	default:
		return false, fmt.Errorf("failed to check the owner of the Secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
}

// errSecretCopyNotControlled is returned when a copy would replace a Secret
// the data plane doesn't control.
var errSecretCopyNotControlled = errors.New("the Secret exists and isn't controlled by the KegDataPlane")

// cleanupServerBundleCopy deletes the copy of the cp server CA bundle of the
// GWAdmin once the data plane no longer needs it, e.g. when its GWAdmin is
// now in its namespace, and its Deployment rolled out without it.
func cleanupServerBundleCopy(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	cp shareddataplane.ResolvedControlPlane,
) error {
	if deps := onPremDependenciesOf(cp); deps != nil && egdp.Namespace != deps.serverBundle.Namespace {
		return nil
	}
	key := client.ObjectKey{Namespace: egdp.Namespace, Name: serverBundleCopyName(egdp)}
	var copied corev1.Secret
	if err := cl.Get(ctx, key, &copied); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(&copied, egdp) {
		return nil
	}
	var d appsv1.Deployment
	switch err := cl.Get(ctx, client.ObjectKeyFromObject(egdp), &d); {
	case apierrors.IsNotFound(err):
	case err != nil:
		return fmt.Errorf("failed to get the Deployment %s: %w", client.ObjectKeyFromObject(egdp), err)
	default:
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.Secret != nil && v.Secret.SecretName == copied.Name {
				return nil
			}
		}
		if !shareddataplane.DeploymentRolledOut(&d) {
			return nil
		}
	}
	if err := cl.Delete(ctx, &copied); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete the copy %s of the cp server CA bundle: %w", key, err)
	}
	log.Debug(logger, "copy of the cp server CA bundle removed", "name", copied.Name)
	return nil
}

// onPremCertificateParentRef returns the parent reference of the
// EventGatewayDataPlaneCertificate of a data plane of the OnPremEventGateway.
func onPremCertificateParentRef(egdp *eventgatewayv1alpha1.KegDataPlane, gateway *eventgatewayv1alpha1.OnPremEventGateway) *commonv1alpha1.GatewayRef {
	return &commonv1alpha1.GatewayRef{
		Group:     eventgatewayv1alpha1.GroupName,
		Kind:      "OnPremEventGateway",
		Name:      gateway.Name,
		Namespace: gatewayNamespaceRef(egdp, gateway),
	}
}

// buildOnPremCertificate builds the EventGatewayDataPlaneCertificate
// registering the client certificate of a data plane on its
// OnPremEventGateway.
func buildOnPremCertificate(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	gateway *eventgatewayv1alpha1.OnPremEventGateway,
	certSecretName, certName string,
) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
	return &configurationv1alpha1.EventGatewayDataPlaneCertificate{
		APIVersion: configurationv1alpha1.GroupVersion.String(),
		Kind:       "EventGatewayDataPlaneCertificate",
		Name:       certName,
		Namespace:  egdp.Namespace,
		Labels:     selectorLabelsForKegDataPlane(egdp),
		Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
			ParentRef: onPremCertificateParentRef(egdp, gateway),
			APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
				Certificate: configurationv1alpha1.SensitiveDataSource{
					Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
						Name: certSecretName,
						Key:  corev1.TLSCertKey,
					},
				},
				Name: certName,
			},
		},
	}
}
