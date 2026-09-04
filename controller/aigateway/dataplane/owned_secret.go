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

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// getCertificateSecret resolves the mTLS client certificate Secret for the
// given AIGatewayDataPlane, honoring spec.certificateSecret.provisioning:
// Manual fetches the user-referenced Secret as-is, Automatic generates and
// manages it. Both only apply when aigatewaycp is non-nil (a KonnectAIGateway
// was resolved): with no control plane to ever use the certificate against,
// provisioning (or even just validating) one would be pure waste. If
// aigatewaycp is nil, (op.Noop, nil, nil) is returned and, if the user did
// configure spec.certificateSecret anyway, the mismatch is surfaced via the
// CertificateProvisioned condition rather than silently ignored; the
// AIGatewayDataPlane is otherwise fully manual, wired entirely via
// spec.deployment.podTemplateSpec.
func (r *Reconciler) getCertificateSecret(
	ctx context.Context,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
) (op.Result, *corev1.Secret, error) {
	cs := aigwdp.Spec.CertificateSecret
	if aigatewaycp == nil {
		if cs != nil {
			apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
				Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
				Status:             metav1.ConditionFalse,
				Reason:             string(aigatewayv1alpha1.CertificateControlPlaneRefMissingReason),
				Message:            aigatewayv1alpha1.CertificateControlPlaneRefMissingMessage,
				ObservedGeneration: aigwdp.Generation,
			})
		}
		return op.Noop, nil, nil
	}
	if cs != nil && cs.Provisioning != nil && *cs.Provisioning == aigatewayv1alpha1.ManualCertificateProvisioning {
		return r.getManualCertificateSecret(ctx, aigwdp)
	}
	return r.ensureCertificateSecret(ctx, aigwdp)
}

// getManualCertificateSecret fetches the Secret referenced by
// spec.certificateSecret.secretRef and validates it contains a usable TLS
// certificate and key. The operator never creates, modifies, rotates, or
// deletes this Secret. The reference is always resolved in the
// AIGatewayDataPlane's own namespace: a Secret can only ever be mounted into
// a Pod's volumes from that Pod's own namespace, so cross-namespace
// references are not supported.
func (r *Reconciler) getManualCertificateSecret(
	ctx context.Context,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (op.Result, *corev1.Secret, error) {
	secretRef := aigwdp.Spec.CertificateSecret.SecretRef
	name := secretRef.Name
	ns := aigwdp.Namespace

	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, secret)
	if err != nil {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.CertificateSecretRefNotFoundReason),
			Message:            aigatewayv1alpha1.CertificateSecretRefNotFoundMessage(name),
			ObservedGeneration: aigwdp.Generation,
		})
		return op.Noop, nil, fmt.Errorf("referenced certificate Secret %s/%s not found: %w", ns, name, err)
	}

	if !secrets.IsTLSSecretValid(secret) {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.CertificateSecretInvalidReason),
			Message:            aigatewayv1alpha1.CertificateSecretInvalidMessage,
			ObservedGeneration: aigwdp.Generation,
		})
		return op.Noop, nil, nil
	}

	apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
		Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(aigatewayv1alpha1.CertificateProvisionedReason),
		Message:            "mTLS certificate Secret referenced and valid",
		ObservedGeneration: aigwdp.Generation,
	})
	return op.Noop, secret, nil
}

// ensureCertificateSecret provisions (or finds) the mTLS client certificate Secret
// for the given AIGatewayDataPlane, signed by the cluster CA.
func (r *Reconciler) ensureCertificateSecret(
	ctx context.Context,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (op.Result, *corev1.Secret, error) {
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey:               consts.SecretProvisioningAutomaticLabelValue,
		consts.SecretAIGatewayDataPlaneCertificateLabel: "true",
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	res, secret, err := secrets.EnsureCertificate(
		ctx,
		aigwdp,
		fmt.Sprintf("%s.%s", aigwdp.Name, aigwdp.Namespace),
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		[]certificatesv1.KeyUsage{
			certificatesv1.UsageKeyEncipherment,
			certificatesv1.UsageDigitalSignature,
			certificatesv1.UsageClientAuth,
		},
		r.Client,
		matchingLabels,
		r.CertTTL,
	)
	if err != nil {
		apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
			Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(aigatewayv1alpha1.UnableToProvisionReason),
			Message:            fmt.Sprintf("failed to provision mTLS certificate Secret: %v", err),
			ObservedGeneration: aigwdp.Generation,
		})
		return op.Noop, nil, err
	}
	apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
		Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(aigatewayv1alpha1.CertificateProvisionedReason),
		Message:            "mTLS certificate Secret provisioned",
		ObservedGeneration: aigwdp.Generation,
	})
	return res, secret, nil
}
