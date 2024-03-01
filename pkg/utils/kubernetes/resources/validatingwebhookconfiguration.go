package resources

import (
	"github.com/samber/lo"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// -----------------------------------------------------------------------------
// ValidatingWebhookConfiguration generators
// -----------------------------------------------------------------------------

// ValidatingWebhookConfigurationBuilder is a helper to generate a ValidatingWebhookConfiguration.
type ValidatingWebhookConfigurationBuilder struct {
	vwc *admissionregistrationv1.ValidatingWebhookConfiguration
}

// NewValidatingWebhookConfigurationBuilder returns builder for ValidatingWebhookConfiguration.
// Check method to learn more about the default values and available options.
func NewValidatingWebhookConfigurationBuilder(webhookName string) *ValidatingWebhookConfigurationBuilder {
	namespacedScope := admissionregistrationv1.NamespacedScope
	return &ValidatingWebhookConfigurationBuilder{
		vwc: &admissionregistrationv1.ValidatingWebhookConfiguration{
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
								APIVersions: []string{"v1beta1"},
								Resources:   []string{"controlplanes"},
								Scope:       &namespacedScope,
							},
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
								admissionregistrationv1.Update,
							},
						},
					},
					AdmissionReviewVersions: []string{"v1", "v1beta1"},
					SideEffects:             lo.ToPtr(admissionregistrationv1.SideEffectClassNone),
					TimeoutSeconds:          lo.ToPtr(int32(5)),
				},
			},
		},
	}
}

// WithClientConfigKubernetesService sets the client config to use a Kubernetes service.
func (v *ValidatingWebhookConfigurationBuilder) WithClientConfigKubernetesService(svc k8stypes.NamespacedName) *ValidatingWebhookConfigurationBuilder {
	for i := range v.vwc.Webhooks {
		v.vwc.Webhooks[i].ClientConfig.Service = &admissionregistrationv1.ServiceReference{
			Namespace: svc.Namespace,
			Name:      svc.Name,
			Path:      lo.ToPtr("/validate"),
		}
	}
	return v
}

// WithClientConfigURL sets the client config to use a URL.
func (v *ValidatingWebhookConfigurationBuilder) WithClientConfigURL(url string) *ValidatingWebhookConfigurationBuilder {
	for i := range v.vwc.Webhooks {
		v.vwc.Webhooks[i].ClientConfig.URL = &url
	}
	return v
}

// WithCABundle sets the CA bundle.
func (v *ValidatingWebhookConfigurationBuilder) WithCABundle(caBundle []byte) *ValidatingWebhookConfigurationBuilder {
	for i := range v.vwc.Webhooks {
		v.vwc.Webhooks[i].ClientConfig.CABundle = caBundle
	}
	return v
}

// WithScopeAll sets the scope for all namespaces (default for the builder is namespace code).
func (v *ValidatingWebhookConfigurationBuilder) WithScopeAllNamespaces() *ValidatingWebhookConfigurationBuilder {
	for i := range v.vwc.Webhooks {
		for j := range v.vwc.Webhooks[i].Rules {
			v.vwc.Webhooks[i].Rules[j].Scope = lo.ToPtr(admissionregistrationv1.AllScopes)
		}
	}
	return v
}

// Build returns the ValidatingWebhookConfiguration.
func (v *ValidatingWebhookConfigurationBuilder) Build() *admissionregistrationv1.ValidatingWebhookConfiguration {
	return v.vwc
}
