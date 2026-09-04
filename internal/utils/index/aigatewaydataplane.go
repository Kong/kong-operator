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

package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
)

const (
	// IndexFieldAIGatewayDataPlaneOnKonnectAIGateway is the index field for
	// AIGatewayDataPlane -> KonnectAIGateway (via spec.controlPlaneRef.konnectNamespacedRef.name).
	IndexFieldAIGatewayDataPlaneOnKonnectAIGateway = "aiGatewayDataPlaneKonnectAIGatewayRef"
	// IndexFieldAIGatewayDataPlaneOnCertificateSecret is the index field for
	// AIGatewayDataPlane -> Secret (via spec.certificateSecret.secretRef.name),
	// used to reconcile when a manually-referenced certificate Secret changes.
	IndexFieldAIGatewayDataPlaneOnCertificateSecret = "aiGatewayDataPlaneCertificateSecretRef"
)

// OptionsForAIGatewayDataPlane returns required Index options for the AIGatewayDataPlane controller.
func OptionsForAIGatewayDataPlane() []Option {
	return []Option{
		{
			Object:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			Field:          IndexFieldAIGatewayDataPlaneOnKonnectAIGateway,
			ExtractValueFn: aiGatewayDataPlaneControlPlaneRef,
		},
		{
			Object:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			Field:          IndexFieldAIGatewayDataPlaneOnCertificateSecret,
			ExtractValueFn: aiGatewayDataPlaneCertificateSecretRef,
		},
	}
}

func aiGatewayDataPlaneCertificateSecretRef(object client.Object) []string {
	aigwdp, ok := object.(*aigatewayv1alpha1.AIGatewayDataPlane)
	if !ok || aigwdp.Spec.CertificateSecret == nil || aigwdp.Spec.CertificateSecret.SecretRef == nil {
		return nil
	}
	secretRef := aigwdp.Spec.CertificateSecret.SecretRef
	return []string{aigwdp.Namespace + "/" + secretRef.Name}
}

func aiGatewayDataPlaneControlPlaneRef(object client.Object) []string {
	aigwdp, ok := object.(*aigatewayv1alpha1.AIGatewayDataPlane)
	if !ok {
		return nil
	}
	if aigwdp.Spec.ControlPlaneRef == nil || aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef == nil {
		return nil
	}
	return []string{aigwdp.Namespace + "/" + aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name}
}
