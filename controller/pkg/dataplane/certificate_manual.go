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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// certificateClockSkewAllowance is how far in the future a manually-provided
// certificate's NotBefore may be and still be accepted. Issuers such as
// cert-manager set NotBefore to the issuance time, so a freshly issued
// certificate must not be rejected just because the operator's clock is
// slightly behind the issuer's.
const certificateClockSkewAllowance = 5 * time.Minute

// maxCertificateRequeue caps the delays derived from a certificate's validity
// period. Far-future dates (e.g. a NotAfter of 9999-12-31) would otherwise
// overflow [time.Duration], and the cap also re-validates long-lived
// certificates at least once a day.
const maxCertificateRequeue = 24 * time.Hour

// RequeueAfterError is returned by certificate resolution when the
// certificate can't be used yet but will become usable on its own at a known
// time (e.g. a certificate whose NotBefore is in the future). The shared
// reconciler requeues the DataPlane after After instead of retrying with
// error backoff; the reason is surfaced through a status condition.
type RequeueAfterError struct {
	// After is the delay after which the DataPlane should be reconciled again.
	After time.Duration
	// Err describes why the certificate can't be used yet.
	Err error
}

// Error returns the reason the certificate can't be used yet.
func (e *RequeueAfterError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying reason the certificate can't be used yet.
func (e *RequeueAfterError) Unwrap() error { return e.Err }

// CertificateChecksum computes a stable checksum of a certificate Secret's
// tls.crt and tls.key content, used to trigger a Deployment rollout when a
// manually-referenced Secret is edited in place.
func CertificateChecksum(secret *corev1.Secret) string {
	h := sha256.New()
	h.Write(secret.Data[corev1.TLSCertKey])
	h.Write(secret.Data[corev1.TLSPrivateKeyKey])
	return hex.EncodeToString(h.Sum(nil))
}

// CertEntityName derives the Konnect certificate CR name from the DataPlane
// name and the certificate content checksum. Naming the entity after the
// content (rather than after the DataPlane name alone) means a certificate
// rotation creates a new CR/Konnect entity instead of overwriting the
// existing one in place, so the previous certificate stays registered and
// trusted by Konnect until it is safe to remove it (see
// CleanupStaleKonnectCertificates). When the DataPlane name must be
// truncated, a hash of the full name is retained to distinguish names with a
// shared prefix.
func CertEntityName(dpName, certChecksum string) string {
	const (
		maxObjectNameLen  = 253
		checksumPrefixLen = 10
		nameHashPrefixLen = 10
	)
	suffix := certChecksum
	if len(suffix) > checksumPrefixLen {
		suffix = suffix[:checksumPrefixLen]
	}
	name := dpName
	if maxNameLen := maxObjectNameLen - 1 - len(suffix); len(name) > maxNameLen {
		nameHash := fmt.Sprintf("%x", sha256.Sum256([]byte(name)))[:nameHashPrefixLen]
		maxNameLen = maxObjectNameLen - 1 - len(nameHash) - 1 - len(suffix)
		name = strings.TrimRight(name[:maxNameLen], ".-")
		return fmt.Sprintf("%s-%s-%s", name, nameHash, suffix)
	}
	return fmt.Sprintf("%s-%s", name, suffix)
}

// ManualCertificateConditions carries the condition type, reasons and
// messages GetManualCertificateSecret reports on the DataPlane. Values are
// supplied by each specialized controller from its API package constants.
type ManualCertificateConditions struct {
	// Type is the type of the certificate condition.
	Type string
	// ProvisionedReason is the reason used when the Secret is valid.
	ProvisionedReason string
	// UnableToProvisionReason is the reason used when reading the Secret fails.
	UnableToProvisionReason string
	// SecretRefNotFoundReason is the reason used when the Secret does not exist.
	SecretRefNotFoundReason string
	// SecretRefNotFoundMessage formats the message used when the Secret does not exist.
	SecretRefNotFoundMessage func(name string) string
	// SecretInvalidReason is the reason used when the Secret has no usable TLS pair.
	SecretInvalidReason string
	// SecretInvalidMessage is the message used when the Secret has no usable
	// TLS pair. The specific validation failure is appended to it.
	SecretInvalidMessage string
	// SecretOperatorManagedReason is the reason used when the Secret is an
	// operator-provisioned (Automatic) certificate Secret.
	SecretOperatorManagedReason string
	// SecretOperatorManagedMessage is the message used when the Secret is an
	// operator-provisioned (Automatic) certificate Secret.
	SecretOperatorManagedMessage string
}

// GetManualCertificateSecret fetches the user-referenced certificate Secret
// named secretName and validates it contains a usable TLS certificate and
// key, reporting the outcome through conds. The operator never creates,
// modifies, rotates, or deletes this Secret. The reference is always resolved
// in the DataPlane's own namespace: a Secret can only ever be mounted into a
// Pod's volumes from that Pod's own namespace, so cross-namespace references
// are not supported.
func GetManualCertificateSecret(
	ctx context.Context,
	cl client.Client,
	dp Object,
	secretName string,
	conds ManualCertificateConditions,
) (op.Result, *corev1.Secret, error) {
	ns := dp.GetNamespace()

	secret := &corev1.Secret{}
	err := cl.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, secret)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			// A transient API error or an RBAC denial isn't a missing-Secret
			// problem: don't point the user at the label-selector requirement,
			// and surface the error so the reconcile retries with backoff.
			setStatusCondition(dp, metav1.Condition{
				Type:               conds.Type,
				Status:             metav1.ConditionFalse,
				Reason:             conds.UnableToProvisionReason,
				Message:            fmt.Sprintf("failed to read certificate Secret %q: %v", secretName, err),
				ObservedGeneration: dp.GetGeneration(),
			})
			return op.Noop, nil, fmt.Errorf("failed to read certificate Secret %s/%s: %w", ns, secretName, err)
		}
		// A missing referenced Secret is an expected, user-fixable state: the
		// condition points the user at the reference, and the Secret watch
		// re-triggers the reconcile once the Secret appears, so there is no
		// need to retry with error backoff.
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.SecretRefNotFoundReason,
			Message:            conds.SecretRefNotFoundMessage(secretName),
			ObservedGeneration: dp.GetGeneration(),
		})
		return op.Noop, nil, nil
	}

	// An operator-provisioned Secret is owned and garbage-collected by the
	// operator (e.g. removed by CleanupStaleAutomaticCertificateSecret once a
	// switch to Manual completes), so it can never serve as a user-owned
	// Manual certificate.
	if secret.Labels[consts.SecretProvisioningLabelKey] == consts.SecretProvisioningAutomaticLabelValue {
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.SecretOperatorManagedReason,
			Message:            conds.SecretOperatorManagedMessage,
			ObservedGeneration: dp.GetGeneration(),
		})
		return op.Noop, nil, nil
	}

	if err := validateManualCertificate(secret, time.Now()); err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               conds.Type,
			Status:             metav1.ConditionFalse,
			Reason:             conds.SecretInvalidReason,
			Message:            fmt.Sprintf("%s: %v", conds.SecretInvalidMessage, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		// A certificate that isn't valid yet becomes usable on its own:
		// requeue for that moment, as no watch event will fire for it.
		if notYetValid, ok := errors.AsType[*RequeueAfterError](err); ok {
			return op.Noop, nil, notYetValid
		}
		return op.Noop, nil, nil
	}

	setStatusCondition(dp, metav1.Condition{
		Type:               conds.Type,
		Status:             metav1.ConditionTrue,
		Reason:             conds.ProvisionedReason,
		Message:            "mTLS certificate Secret referenced and valid",
		ObservedGeneration: dp.GetGeneration(),
	})
	return op.Noop, secret, nil
}

// validateManualCertificate checks that the Secret holds a usable mTLS client
// certificate: PEM-encoded tls.crt and tls.key that form a matching X.509 key
// pair, with a leaf certificate that is valid as of now. A NotBefore up to
// certificateClockSkewAllowance in the future is tolerated; one further in the
// future yields a *RequeueAfterError carrying the delay until it is accepted.
func validateManualCertificate(secret *corev1.Secret, now time.Time) error {
	if !secrets.IsTLSSecretValid(secret) {
		return errors.New("tls.crt and tls.key must both be present and PEM-encoded")
	}
	leaf, err := parseLeafCertificate(secret)
	if err != nil {
		return err
	}
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if acceptableFrom := leaf.NotBefore.Add(-certificateClockSkewAllowance); now.Before(acceptableFrom) {
		return &RequeueAfterError{
			After: min(acceptableFrom.Sub(now), maxCertificateRequeue),
			Err:   fmt.Errorf("certificate not valid before %s", leaf.NotBefore.UTC().Format(time.RFC3339)),
		}
	}
	return nil
}

// parseLeafCertificate parses the Secret's tls.crt/tls.key as an X.509 key
// pair and returns its leaf certificate.
func parseLeafCertificate(secret *corev1.Secret) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return nil, fmt.Errorf("tls.crt and tls.key do not form a valid key pair: %w", err)
	}
	if pair.Leaf != nil {
		return pair.Leaf, nil
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse tls.crt: %w", err)
	}
	return leaf, nil
}

// manualCertificateExpiryRequeue returns the delay after which the DataPlane
// must be reconciled again so that a user-owned certificate Secret's expiry
// is detected and reported (at most maxCertificateRequeue), or 0 when no such
// requeue is needed. Secret changes trigger a reconcile through the Secret
// watch, but the passage of time doesn't. Operator-provisioned Secrets are not
// validated, so they never need it.
func manualCertificateExpiryRequeue(secret *corev1.Secret, now time.Time) time.Duration {
	if secret == nil || secret.Labels[consts.SecretProvisioningLabelKey] == consts.SecretProvisioningAutomaticLabelValue {
		return 0
	}
	leaf, err := parseLeafCertificate(secret)
	if err != nil || !now.Before(leaf.NotAfter) {
		return 0
	}
	// Requeue just past NotAfter, so the certificate is already expired when
	// it is validated again. The cap is applied first, so the extra second
	// can't overflow.
	untilExpiry := leaf.NotAfter.Sub(now)
	if untilExpiry >= maxCertificateRequeue {
		return maxCertificateRequeue
	}
	return untilExpiry + time.Second
}

// CleanupStaleAutomaticCertificateSecret deletes the operator-provisioned
// Automatic certificate Secret(s) owned by dp and marked with labelKey, if
// any still exist after a switch to Manual provisioning. It must only be
// called once the Deployment rollout onto the Manual Secret is confirmed
// complete, so that no running replica is left depending on a Secret this
// removes. Switching back to Automatic later simply provisions a new one;
// nothing guarantees the switch back ever happens, so the Secret can't be
// left around indefinitely waiting for it (owner-reference GC alone would
// only remove it when the DataPlane itself is deleted).
func CleanupStaleAutomaticCertificateSecret(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dp client.Object,
	labelKey string,
) error {
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
		labelKey:                          "true",
	}
	stale, err := k8sutils.ListSecretsForOwner(ctx, cl, dp.GetUID(), client.InNamespace(dp.GetNamespace()), matchingLabels)
	if err != nil {
		return fmt.Errorf("failed to list automatic certificate Secrets for %s/%s: %w",
			dp.GetNamespace(), dp.GetName(), err)
	}
	for i := range stale {
		secret := &stale[i]
		if err := cl.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete stale automatic certificate Secret %s/%s: %w",
				secret.Namespace, secret.Name, err)
		}
		log.Debug(logger, "stale automatic certificate Secret removed after switch to Manual provisioning", "name", secret.Name)
	}
	return nil
}

// CleanupStaleKonnectCertificates deletes every Konnect certificate object of
// kind certKind controlled by dp except the one named currentCertName. certs
// is an empty list of that kind (e.g. &AIGatewayDataPlaneCertificateList{})
// used to list the candidates. It must
// only be called once the Deployment rollout using the current certificate is
// confirmed complete, so that no running replica is left depending on a
// certificate this removes from Konnect (deleting the CR deprovisions the
// underlying certificate via the Konnect entity finalizer).
func CleanupStaleKonnectCertificates(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dp client.Object,
	certs client.ObjectList,
	certKind string,
	currentCertName string,
) error {
	// Select by owner reference rather than the managed-by labels: certificates
	// created before those labels existed still carry an owner reference (see
	// SetOwnerForObject in the shared reconciler) and would otherwise be
	// invisible to this List, left orphaned in Konnect forever.
	if err := cl.List(ctx, certs, client.InNamespace(dp.GetNamespace())); err != nil {
		return fmt.Errorf("failed to list %ss for %s/%s: %w",
			certKind, dp.GetNamespace(), dp.GetName(), err)
	}
	items, err := meta.ExtractList(certs)
	if err != nil {
		return fmt.Errorf("failed to extract %s list items for %s/%s: %w",
			certKind, dp.GetNamespace(), dp.GetName(), err)
	}

	for _, item := range items {
		stale, ok := item.(client.Object)
		if !ok || stale.GetName() == currentCertName || !metav1.IsControlledBy(stale, dp) {
			continue
		}
		if err := cl.Delete(ctx, stale); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete stale %s %s/%s: %w",
				certKind, stale.GetNamespace(), stale.GetName(), err)
		}
		log.Debug(logger, "stale "+certKind+" removed", "name", stale.GetName())
	}
	return nil
}
