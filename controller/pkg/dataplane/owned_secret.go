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

	"github.com/go-logr/logr"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// certificateSecretParams carries the type specific bits of a TLS certificate
// Secret provisioned for a DataPlane: how the Secret is identified and signed
// (label key, subject, key usages) and how its provisioning surfaces in the
// DataPlane status (condition type and reason; Description is used to build
// the condition messages).
type certificateSecretParams struct {
	// LabelKey marks the provisioned Secret.
	LabelKey string
	// Subject is the certificate subject, used as the Common Name and
	// the sole DNS SAN.
	Subject string
	// Usages are the X.509 key usages of the certificate (e.g. client
	// authentication for a Konnect mTLS certificate, server authentication
	// for an Admin API certificate).
	Usages []certificatesv1.KeyUsage
	// ConditionType is the type of the provisioning condition.
	ConditionType string
	// ConditionReason is the reason used once the Secret is provisioned.
	ConditionReason string
	// Description is the human-readable description used in the condition
	// messages (e.g. "mTLS certificate Secret").
	Description string
}

// ensureCertificateSecretFor provisions (or finds) a TLS certificate Secret
// for the given DataPlane as configured by params, signed by the cluster CA,
// surfacing the provisioning result through the params condition.
func (r *Reconciler[T, Cert]) ensureCertificateSecretFor(
	ctx context.Context,
	dp T,
	params certificateSecretParams,
) (op.Result, *corev1.Secret, error) {
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
		params.LabelKey:                   "true",
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	res, secret, err := r.Config.Certificate.Ensure(
		ctx,
		dp,
		params.Subject,
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		params.Usages,
		r.Client,
		matchingLabels,
		r.CertTTL,
	)
	if err != nil {
		setStatusCondition(dp, metav1.Condition{
			Type:               params.ConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.UnableToProvisionReason,
			Message:            fmt.Sprintf("failed to provision %s: %v", params.Description, err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return op.Noop, nil, err
	}
	setStatusCondition(dp, metav1.Condition{
		Type:               params.ConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             params.ConditionReason,
		Message:            fmt.Sprintf("%s provisioned", params.Description),
		ObservedGeneration: dp.GetGeneration(),
	})
	return res, secret, nil
}

// ensureCertificateSecret provisions (or finds) the mTLS client certificate
// Secret for the given DataPlane, signed by the cluster CA.
func (r *Reconciler[T, Cert]) ensureCertificateSecret(
	ctx context.Context,
	dp T,
) (op.Result, *corev1.Secret, error) {
	return r.ensureCertificateSecretFor(ctx, dp, certificateSecretParams{
		LabelKey:        r.Config.Certificate.LabelKey,
		Subject:         fmt.Sprintf("%s.%s", dp.GetName(), dp.GetNamespace()),
		Usages:          []certificatesv1.KeyUsage{certificatesv1.UsageKeyEncipherment, certificatesv1.UsageDigitalSignature, certificatesv1.UsageClientAuth},
		ConditionType:   r.Config.Conditions.CertificateProvisionedType,
		ConditionReason: r.Config.Conditions.CertificateProvisionedReason,
		Description:     "mTLS certificate Secret",
	})
}

// ensureAdminCertificateSecret provisions (or finds) the Admin API TLS server
// certificate Secret for the given DataPlane, signed by the cluster CA and
// labeled with AdminAPI.CertificateLabelKey.
func (r *Reconciler[T, Cert]) ensureAdminCertificateSecret(
	ctx context.Context,
	dp T,
	adminAPI *AdminAPIConfig[T],
) (op.Result, *corev1.Secret, error) {
	return r.ensureCertificateSecretFor(ctx, dp, certificateSecretParams{
		LabelKey:        adminAPI.CertificateLabelKey,
		Subject:         adminAPI.certificateSubject(dp),
		Usages:          []certificatesv1.KeyUsage{certificatesv1.UsageKeyEncipherment, certificatesv1.UsageDigitalSignature, certificatesv1.UsageServerAuth},
		ConditionType:   r.Config.Conditions.AdminCertificateProvisionedType,
		ConditionReason: r.Config.Conditions.AdminCertificateProvisionedReason,
		Description:     "Admin API certificate Secret",
	})
}

// deleteAdminCertificateSecretsIfOwned removes the Admin API certificate
// Secret(s) owned by the given DataPlane, i.e. leftovers from an earlier
// reconcile in which AdminAPI.Enabled was true (e.g. the control
// plane reference changed to a kind that doesn't consume the Admin API, or
// was removed). The AdminCertificateProvisioned condition is removed as well,
// since it no longer applies. Must only be called after the Deployment has
// been rebuilt without the admin certificate wiring: deleting the Secret(s)
// earlier would leave the Deployment mounting a Secret that no longer exists.
func (r *Reconciler[T, Cert]) deleteAdminCertificateSecretsIfOwned(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	adminAPI *AdminAPIConfig[T],
) error {
	matchingLabels := k8sresources.GetManagedLabelForOwner(dp)
	matchingLabels[consts.SecretProvisioningLabelKey] = consts.SecretProvisioningAutomaticLabelValue
	matchingLabels[adminAPI.CertificateLabelKey] = "true"
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	secrets, err := k8sutils.ListSecretsForOwner(ctx, r.Client, dp.GetUID(), matchingLabels)
	if err != nil {
		return fmt.Errorf("failed listing Admin API certificate Secrets for %s %s/%s: %w",
			r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	for i := range secrets {
		secret := &secrets[i]
		if !metav1.IsControlledBy(secret, dp) {
			continue
		}
		if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			r.EventRecorder.Eventf(dp, nil, corev1.EventTypeWarning, "SecretFailed", "DeleteSecret",
				"Failed to delete Admin API certificate Secret: %v", err)
			return fmt.Errorf("failed to delete Admin API certificate Secret for %s %s/%s: %w",
				r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
		}
		log.Debug(logger, "Admin API certificate Secret removed (no longer enabled)", "name", secret.Name)
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "SecretDeleted", "DeleteSecret",
			"Admin API certificate Secret %s deleted", secret.Name)
	}
	removeStatusCondition(dp, r.Config.Conditions.AdminCertificateProvisionedType)
	return nil
}
