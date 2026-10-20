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
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
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
// Manual fetches the user-referenced Secret as-is, Automatic (the default)
// falls back to the shared operator-managed provisioning.
func resolveCertificateSecret(
	ctx context.Context,
	cl client.Client,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	_ shareddataplane.ResolvedControlPlane,
	resolveAutomatic func(ctx context.Context, dp *eventgatewayv1alpha1.KegDataPlane) (op.Result, *corev1.Secret, error),
) (op.Result, *corev1.Secret, error) {
	if isManualProvisioning(egdp) {
		return shareddataplane.GetManualCertificateSecret(ctx, cl, egdp, egdp.Spec.CertificateSecret.SecretRef.Name, manualCertificateConditions)
	}
	return resolveAutomatic(ctx, egdp)
}

// isManualProvisioning reports whether the KegDataPlane is configured with a
// manually-provisioned certificate Secret.
func isManualProvisioning(egdp *eventgatewayv1alpha1.KegDataPlane) bool {
	cs := egdp.Spec.CertificateSecret
	return cs != nil && cs.Provisioning != nil && *cs.Provisioning == eventgatewayv1alpha1.ManualCertificateProvisioning &&
		cs.SecretRef != nil
}
