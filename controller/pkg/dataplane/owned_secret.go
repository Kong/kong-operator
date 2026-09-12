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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// ensureCertificateSecret provisions (or finds) the mTLS client certificate
// Secret for the given DataPlane, signed by the cluster CA.
func (r *Reconciler[T, CP, Cert]) ensureCertificateSecret(
	ctx context.Context,
	dp T,
) (op.Result, *corev1.Secret, error) {
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey: consts.SecretProvisioningAutomaticLabelValue,
		r.Config.CertificateLabelKey:      "true",
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	res, secret, err := r.Config.EnsureCertificate(
		ctx,
		dp,
		fmt.Sprintf("%s.%s", dp.GetName(), dp.GetNamespace()),
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
		setStatusCondition(dp, metav1.Condition{
			Type:               r.Config.Conditions.CertificateProvisionedType,
			Status:             metav1.ConditionFalse,
			Reason:             r.Config.Conditions.UnableToProvisionReason,
			Message:            fmt.Sprintf("failed to provision mTLS certificate Secret: %v", err),
			ObservedGeneration: dp.GetGeneration(),
		})
		return op.Noop, nil, err
	}
	setStatusCondition(dp, metav1.Condition{
		Type:               r.Config.Conditions.CertificateProvisionedType,
		Status:             metav1.ConditionTrue,
		Reason:             r.Config.Conditions.CertificateProvisionedReason,
		Message:            "mTLS certificate Secret provisioned",
		ObservedGeneration: dp.GetGeneration(),
	})
	return res, secret, nil
}
