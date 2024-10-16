package resources

import (
	"github.com/samber/lo"
	admregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateValidatingWebhookConfigurationForControlPlane generates a ValidatingWebhookConfiguration for a control plane
// based on the control plane version. It also overrides all webhooks' client configurations with the provided service
// details.
func GenerateMutatingWebhookConfigurationMeshForControlPlane(webhookName string, clientConfig admregv1.WebhookClientConfig) *admregv1.MutatingWebhookConfiguration {
	return &admregv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kuma-admission-mutating-webhook-configuration",
		},
		Webhooks: []admregv1.MutatingWebhook{
			{
				Name: "mesh.defaulter.kuma-admission.kuma.io",
				ClientConfig: admregv1.WebhookClientConfig{
					Service: &admregv1.ServiceReference{
						Name:      "kuma-control-plane",
						Namespace: "kuma-system",
						Path:      lo.ToPtr("/default-kuma-io-v1alpha1-mesh"),
						Port:      lo.ToPtr(int32(443)),
					},
				},
				FailurePolicy:           lo.ToPtr(admregv1.Fail),
				MatchPolicy:             lo.ToPtr(admregv1.Equivalent),
				NamespaceSelector:       &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"kube-system"}}}},
				ReinvocationPolicy:      lo.ToPtr(admregv1.NeverReinvocationPolicy),
				AdmissionReviewVersions: []string{"v1"},
				Rules: []admregv1.RuleWithOperations{
					{
						Operations: []admregv1.OperationType{admregv1.Create, admregv1.Update},
						Rule: admregv1.Rule{
							APIGroups:   []string{"kuma.io"},
							APIVersions: []string{"v1alpha1"},
							Resources: []string{
								"meshes", "meshgateways", "meshaccesslogs", "meshcircuitbreakers", "meshfaultinjections",
								"meshhealthchecks", "meshhttproutes", "meshloadbalancingstrategies", "meshmetrics",
								"meshpassthroughs", "meshproxypatches", "meshratelimits", "meshretries", "meshtcproutes",
								"meshtimeouts", "meshtraces", "meshtrafficpermissions", "hostnamegenerators", "meshexternalservices", "meshservices",
							},
							Scope: lo.ToPtr(admregv1.AllScopes),
						},
					},
				},
				SideEffects:    lo.ToPtr(admregv1.SideEffectClassNone),
				TimeoutSeconds: lo.ToPtr(int32(10)),
			},
			{
				Name: "owner-reference.kuma-admission.kuma.io",
				ClientConfig: admregv1.WebhookClientConfig{
					Service: &admregv1.ServiceReference{
						Name:      "kuma-control-plane",
						Namespace: "kuma-system",
						Path:      lo.ToPtr("/owner-reference-kuma-io-v1alpha1"),
						Port:      lo.ToPtr(int32(443)),
					},
				},
				FailurePolicy:           lo.ToPtr(admregv1.Fail),
				MatchPolicy:             lo.ToPtr(admregv1.Equivalent),
				NamespaceSelector:       &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"kube-system"}}}},
				ReinvocationPolicy:      lo.ToPtr(admregv1.NeverReinvocationPolicy),
				AdmissionReviewVersions: []string{"v1"},
				Rules: []admregv1.RuleWithOperations{
					{
						Operations: []admregv1.OperationType{admregv1.Create},
						Rule: admregv1.Rule{
							APIGroups:   []string{"kuma.io"},
							APIVersions: []string{"v1alpha1"},
							Resources: []string{
								"circuitbreakers", "externalservices", "faultinjections", "healthchecks", "meshgateways",
								"meshgatewayroutes", "proxytemplates", "ratelimits", "retries", "timeouts", "trafficlogs",
								"trafficpermissions", "trafficroutes", "traffictraces", "virtualoutbounds", "meshaccesslogs",
								"meshcircuitbreakers", "meshfaultinjections", "meshhealthchecks", "meshhttproutes", "meshloadbalancingstrategies",
								"meshmetrics", "meshpassthroughs", "meshproxypatches", "meshratelimits", "meshretries", "meshtcproutes",
								"meshtimeouts", "meshtraces", "meshtrafficpermissions", "hostnamegenerators", "meshexternalservices", "meshservices",
							},
							Scope: lo.ToPtr(admregv1.AllScopes),
						},
					},
				},
				SideEffects:    lo.ToPtr(admregv1.SideEffectClassNone),
				TimeoutSeconds: lo.ToPtr(int32(10)),
			},
			{
				Name: "namespace-kuma-injector.kuma.io",
				ClientConfig: admregv1.WebhookClientConfig{
					Service: &admregv1.ServiceReference{
						Name:      "kuma-control-plane",
						Namespace: "kuma-system",
						Path:      lo.ToPtr("/inject-sidecar"),
						Port:      lo.ToPtr(int32(443)),
					},
				},
				FailurePolicy: lo.ToPtr(admregv1.Fail),
				MatchPolicy:   lo.ToPtr(admregv1.Equivalent),
				NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"kube-system"}},
					{Key: "kuma.io/sidecar-injection", Operator: metav1.LabelSelectorOpIn, Values: []string{"enabled", "true"}},
				}},
				ReinvocationPolicy:      lo.ToPtr(admregv1.NeverReinvocationPolicy),
				AdmissionReviewVersions: []string{"v1"},
				Rules: []admregv1.RuleWithOperations{
					{
						Operations: []admregv1.OperationType{admregv1.Create},
						Rule: admregv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       lo.ToPtr(admregv1.AllScopes),
						},
					},
				},
				SideEffects:    lo.ToPtr(admregv1.SideEffectClassNone),
				TimeoutSeconds: lo.ToPtr(int32(10)),
			},
			{
				Name: "pods-kuma-injector.kuma.io",
				ClientConfig: admregv1.WebhookClientConfig{
					Service: &admregv1.ServiceReference{
						Name:      "kuma-control-plane",
						Namespace: "kuma-system",
						Path:      lo.ToPtr("/inject-sidecar"),
						Port:      lo.ToPtr(int32(443)),
					},
				},
				FailurePolicy:           lo.ToPtr(admregv1.Fail),
				MatchPolicy:             lo.ToPtr(admregv1.Equivalent),
				NamespaceSelector:       &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: []string{"kube-system"}}}},
				ObjectSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"kuma.io/sidecar-injection": "enabled"}},
				ReinvocationPolicy:      lo.ToPtr(admregv1.NeverReinvocationPolicy),
				AdmissionReviewVersions: []string{"v1"},
				Rules: []admregv1.RuleWithOperations{
					{
						Operations: []admregv1.OperationType{admregv1.Create},
						Rule: admregv1.Rule{
							APIGroups:   []string{""},
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
							Scope:       lo.ToPtr(admregv1.AllScopes),
						},
					},
				},
				SideEffects:    lo.ToPtr(admregv1.SideEffectClassNone),
				TimeoutSeconds: lo.ToPtr(int32(10)),
			},
		},
	}
}
