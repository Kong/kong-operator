package resources

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// -----------------------------------------------------------------------------
// ValidatingWebhookConfiguration generators
// -----------------------------------------------------------------------------

// GenerateNewValidatingWebhookConfiguration is a helper to generate a ValidatingWebhookConfiguration
func GenerateNewValidatingWebhookConfiguration(serviceNamespace, serviceName, webhookName string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	namespacedScope := admissionregistrationv1.NamespacedScope
	sideEffect := admissionregistrationv1.SideEffectClassNone

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name: webhookName,
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"gateway-operator.konghq.com"},
							APIVersions: []string{"v1beta1"},
							Resources:   []string{"dataplanes"},
							Scope:       &namespacedScope,
						},
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
					},
					{
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"gateway-operator.konghq.com"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{"controlplanes"},
							Scope:       &namespacedScope,
						},
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
					},
				},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Namespace: serviceNamespace,
						Name:      serviceName,
						Path:      pointer.String("/validate"),
					},
				},
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				SideEffects:             &sideEffect,
				TimeoutSeconds:          pointer.Int32(5),
			},
		},
	}
}
