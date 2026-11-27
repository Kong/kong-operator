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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// manualCertificateConditions are the KegDataPlane conditions reported for a
// manually-referenced certificate Secret.
var manualCertificateConditions = shareddataplane.ManualCertificateConditions{
	Type:                     string(eventgatewayv1alpha1.CertificateProvisionedType),
	ProvisionedReason:        string(eventgatewayv1alpha1.CertificateProvisionedReason),
	UnableToProvisionReason:  string(eventgatewayv1alpha1.UnableToProvisionReason),
	SecretRefNotFoundReason:  string(eventgatewayv1alpha1.CertificateSecretRefNotFoundReason),
	SecretRefNotFoundMessage: eventgatewayv1alpha1.CertificateSecretRefNotFoundMessage,
	SecretInvalidReason:      string(eventgatewayv1alpha1.CertificateSecretInvalidReason),
	SecretInvalidMessage:     eventgatewayv1alpha1.CertificateSecretInvalidMessage,

	SecretOperatorManagedReason:  string(eventgatewayv1alpha1.CertificateSecretRefOperatorManagedReason),
	SecretOperatorManagedMessage: eventgatewayv1alpha1.CertificateSecretRefOperatorManagedMessage,
}

// resolveCertificateSecret resolves the mTLS client certificate Secret for
// the given KegDataPlane, honoring spec.certificateSecret.provisioning:
// Manual fetches the user-referenced Secret as-is, and, for an
// OnPremEventGateway, checks a cp client CA gw-admin trusts issued it.
// Automatic (the default) falls back to the shared operator-managed
// provisioning, from the cluster CA, or, for an OnPremEventGateway, from the
// cp client CA of its GWAdmin. Either way, a data plane of an
// OnPremEventGateway gets the cp server CA bundle of the GWAdmin first.
func (r *Reconciler) resolveCertificateSecret(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	cp shareddataplane.ResolvedControlPlane,
	resolveAutomatic func(ctx context.Context, dp *eventgatewayv1alpha1.KegDataPlane) (op.Result, *corev1.Secret, error),
) (op.Result, *corev1.Secret, error) {
	deps := onPremDependenciesOf(cp)
	if deps != nil {
		// The Deployment is built on a later reconcile when the copy changes:
		// the watch on the owned Secret triggers it.
		if res, err := r.ensureServerBundleCopy(ctx, cl, egdp, deps); err != nil || res != op.Noop {
			return res, nil, err
		}
	}
	if isManualProvisioning(egdp) {
		before := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.CertificateProvisionedType)).DeepCopy()
		res, secret, err := shareddataplane.GetManualCertificateSecret(ctx, cl, egdp, egdp.Spec.CertificateSecret.SecretRef.Name, manualCertificateConditions)
		// A copy is the operator's, deleted when no longer needed.
		if secret != nil && secret.Labels[secretCopyLabel] != "" {
			setCertificateProvisioned(egdp, metav1.ConditionFalse, eventgatewayv1alpha1.CertificateSecretRefOperatorManagedReason,
				eventgatewayv1alpha1.CertificateSecretRefOperatorManagedMessage)
			keepCertificateProvisionedTransition(egdp, before)
			markManualCertificateNotRegistered(egdp)
			return op.Noop, nil, nil
		}
		if deps == nil {
			return res, secret, err
		}
		// An error tells nothing about the certificate of the Secret.
		if err != nil {
			return res, secret, err
		}
		if secret == nil {
			markManualCertificateNotRegistered(egdp)
			return res, nil, nil
		}
		now := time.Now()
		if secret = checkManualOnPremCertificate(egdp, deps, secret, now); secret == nil {
			keepCertificateProvisionedTransition(egdp, before)
			markManualCertificateNotRegistered(egdp)
			return res, nil, nil
		}
		// The Deployment is built on a later reconcile when the copy changes:
		// the watch on the owned Secret triggers it.
		copyResult, copied, err := r.ensureManualCertificateCopy(ctx, cl, egdp, secret)
		if err != nil || copyResult != op.Noop {
			keepCertificateProvisionedTransition(egdp, before)
			return copyResult, nil, err
		}
		r.warnManualCertificateExpiring(ctx, cl, egdp, deps, egdp.Spec.CertificateSecret.SecretRef.Name, before, now)
		return res, copied, nil
	}
	if deps != nil {
		return r.ensureOnPremCertificate(ctx, cl, egdp, deps)
	}
	return resolveAutomatic(ctx, egdp)
}

// markManualCertificateNotRegistered makes the GWAdminCertificateRegistered
// condition of a data plane say so when its Manual Secret holds no usable
// certificate: registering it doesn't happen then, and the condition would keep
// speaking of the certificate the Secret held before, or of an earlier outage of
// the GWAdmin. A data plane that never registered one has no such condition.
func markManualCertificateNotRegistered(egdp *eventgatewayv1alpha1.KegDataPlane) {
	if apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.GWAdminCertificateRegisteredType)) == nil {
		return
	}
	apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
		Type:               string(eventgatewayv1alpha1.GWAdminCertificateRegisteredType),
		Status:             metav1.ConditionUnknown,
		Reason:             string(eventgatewayv1alpha1.GWAdminCertificateNotProgrammedReason),
		Message:            "The Manual client certificate Secret holds no usable certificate to register: see the CertificateProvisioned condition",
		ObservedGeneration: egdp.GetGeneration(),
	})
}

// keepCertificateProvisionedTransition restores when the CertificateProvisioned
// condition last changed, as it was before, when its status is the same again:
// GetManualCertificateSecret sets it True before the on-prem checks of a Manual
// certificate may set it False, and each of those moves resets its
// LastTransitionTime.
func keepCertificateProvisionedTransition(egdp *eventgatewayv1alpha1.KegDataPlane, before *metav1.Condition) {
	current := apimeta.FindStatusCondition(egdp.Status.Conditions, string(eventgatewayv1alpha1.CertificateProvisionedType))
	if before != nil && current != nil && current.Status == before.Status {
		current.LastTransitionTime = before.LastTransitionTime
	}
}

// isManualProvisioning reports whether the KegDataPlane is configured with a
// manually-provisioned certificate Secret.
func isManualProvisioning(egdp *eventgatewayv1alpha1.KegDataPlane) bool {
	cs := egdp.Spec.CertificateSecret
	return cs != nil && cs.Provisioning != nil && *cs.Provisioning == eventgatewayv1alpha1.ManualCertificateProvisioning &&
		cs.SecretRef != nil
}

// certificateSecretLabels returns the labels of the client certificate Secrets
// the operator issues a data plane: from the cluster CA for a
// KonnectEventGateway, as the shared reconciler provisions them, or from the
// cp client CA of the GWAdmin of an OnPremEventGateway.
func (r *Reconciler) certificateSecretLabels(egdp *eventgatewayv1alpha1.KegDataPlane) client.MatchingLabels {
	labels := k8sresources.GetManagedLabelForOwner(egdp)
	labels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
	labels[consts.SecretKEGDataPlaneCertificateLabel] = "true"
	if r.SecretLabelSelector != "" {
		labels[r.SecretLabelSelector] = "true"
	}
	return labels
}

// listCertificateSecrets lists the client certificate Secrets the operator
// issued a data plane: the current one, and, of an OnPremEventGateway, those
// it replaced, kept until the Deployment rolled out onto it.
func (r *Reconciler) listCertificateSecrets(ctx context.Context, cl client.Reader, egdp *eventgatewayv1alpha1.KegDataPlane) ([]corev1.Secret, error) {
	owned, err := k8sutils.ListSecretsForOwner(ctx, cl, egdp.GetUID(), client.InNamespace(egdp.Namespace), r.certificateSecretLabels(egdp))
	if err != nil {
		return nil, fmt.Errorf("failed to list the mTLS certificate Secrets of %s/%s: %w", egdp.Namespace, egdp.Name, err)
	}
	return owned, nil
}

// deleteCertificateSecrets deletes the client certificate Secrets the operator
// created for a data plane: those it issued (see listCertificateSecrets), and,
// of an OnPremEventGateway, its copies of the Manual ones. A Manual Secret of
// the user stays. The Deployment, no longer reconciled while the data plane is
// revoked, still mounts them: its running pods keep their certificate, which
// the gateway no longer trusts, and a pod started meanwhile waits for its
// Secret. Once a KongReferenceGrant lets the data plane in again, a
// certificate is issued or copied again, and the Deployment rolls out onto it.
func (r *Reconciler) deleteCertificateSecrets(
	ctx context.Context, cl client.Client, egdp *eventgatewayv1alpha1.KegDataPlane,
) error {
	owned, err := r.listCertificateSecrets(ctx, cl, egdp)
	if err != nil {
		return err
	}
	for i := range owned {
		if err := cl.Delete(ctx, &owned[i]); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete the mTLS certificate Secret %s/%s of the revoked data plane %s/%s: %w",
				owned[i].Namespace, owned[i].Name, egdp.Namespace, egdp.Name, err)
		}
	}
	var copies corev1.SecretList
	if err := cl.List(ctx, &copies, client.InNamespace(egdp.Namespace), client.MatchingLabels{secretCopyLabel: secretCopyManualCertificate}); err != nil {
		return fmt.Errorf("failed to list the copies of the Manual client certificates of %s/%s: %w", egdp.Namespace, egdp.Name, err)
	}
	for i := range copies.Items {
		copied := &copies.Items[i]
		if !metav1.IsControlledBy(copied, egdp) {
			continue
		}
		if err := cl.Delete(ctx, copied); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete the copy %s/%s of a Manual client certificate of the revoked data plane %s/%s: %w",
				copied.Namespace, copied.Name, egdp.Namespace, egdp.Name, err)
		}
	}
	return nil
}
