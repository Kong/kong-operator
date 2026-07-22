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
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

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
