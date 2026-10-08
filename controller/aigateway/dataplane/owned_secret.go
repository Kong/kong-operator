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

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
)

// resolveCertificateSecret resolves the mTLS client certificate Secret for
// the given AIGatewayDataPlane, honoring spec.certificateSecret.provisioning:
// Manual fetches the user-referenced Secret as-is, Automatic falls back to
// the shared operator-managed provisioning. Both only apply when a
// Konnect-backed control plane is configured and resolved: the certificate is
// the DataPlane's client identity for the outbound connection to Konnect.
// If no control plane is configured at all, (op.Noop, nil, nil) is returned
// and, if the user did configure spec.certificateSecret anyway, the mismatch is
// surfaced via the CertificateProvisioned condition rather than silently ignored;
// the AIGatewayDataPlane is otherwise fully manual, wired entirely via
// spec.deployment.podTemplateSpec.
func resolveCertificateSecret(
	ctx context.Context,
	cl client.Client,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	cp shareddataplane.ResolvedControlPlane,
	resolveAutomatic func(ctx context.Context, dp *aigatewayv1alpha1.AIGatewayDataPlane) (op.Result, *corev1.Secret, error),
) (op.Result, *corev1.Secret, error) {
	cs := aigwdp.Spec.CertificateSecret
	if cp.Object == nil {
		if cs != nil {
			apimeta.SetStatusCondition(&aigwdp.Status.Conditions, metav1.Condition{
				Type:               string(aigatewayv1alpha1.CertificateProvisionedType),
				Status:             metav1.ConditionFalse,
				Reason:             string(aigatewayv1alpha1.CertificateControlPlaneRefMissingReason),
				Message:            aigatewayv1alpha1.CertificateControlPlaneRefMissingMessage,
				ObservedGeneration: aigwdp.Generation,
			})
		} else {
			// cs was cleared (or never set) while still controlPlaneRef-less:
			// drop any stale condition from an earlier reconcile where cs was
			// non-nil, since nothing else touches CertificateProvisionedType
			// while aigatewaycp stays nil.
			apimeta.RemoveStatusCondition(&aigwdp.Status.Conditions, string(aigatewayv1alpha1.CertificateProvisionedType))
		}
		return op.Noop, nil, nil
	}
	if !cp.IsKonnect {
		// On-prem control plane: no certificate is provisioned or validated.
		return op.Noop, nil, nil
	}
	if isManualProvisioning(aigwdp) {
		return shareddataplane.GetManualCertificateSecret(ctx, cl, aigwdp, aigwdp.Spec.CertificateSecret.SecretRef.Name, manualCertificateConditions)
	}
	return resolveAutomatic(ctx, aigwdp)
}

// manualCertificateConditions are the AIGatewayDataPlane conditions reported
// for a manually-referenced certificate Secret.
var manualCertificateConditions = shareddataplane.ManualCertificateConditions{
	Type:                     string(aigatewayv1alpha1.CertificateProvisionedType),
	ProvisionedReason:        string(aigatewayv1alpha1.CertificateProvisionedReason),
	UnableToProvisionReason:  string(aigatewayv1alpha1.UnableToProvisionReason),
	SecretRefNotFoundReason:  string(aigatewayv1alpha1.CertificateSecretRefNotFoundReason),
	SecretRefNotFoundMessage: aigatewayv1alpha1.CertificateSecretRefNotFoundMessage,
	SecretInvalidReason:      string(aigatewayv1alpha1.CertificateSecretInvalidReason),
	SecretInvalidMessage:     aigatewayv1alpha1.CertificateSecretInvalidMessage,

	SecretOperatorManagedReason:  string(aigatewayv1alpha1.CertificateSecretRefOperatorManagedReason),
	SecretOperatorManagedMessage: aigatewayv1alpha1.CertificateSecretRefOperatorManagedMessage,
}

// isManualProvisioning reports whether the AIGatewayDataPlane is configured
// with a manually-provisioned certificate Secret.
func isManualProvisioning(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) bool {
	cs := aigwdp.Spec.CertificateSecret
	return cs != nil && cs.Provisioning != nil && *cs.Provisioning == aigatewayv1alpha1.ManualCertificateProvisioning &&
		cs.SecretRef != nil
}
