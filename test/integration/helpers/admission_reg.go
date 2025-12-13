package helpers

import (
	"context"
	"fmt"
	"os"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/kubectl"
	admregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/ingress-controller/test/helpers/certificate"
	"github.com/kong/kong-operator/test/helpers/kcfg"
)

// ensureAdmissionRegistration registers a validating webhook for the given configuration, it validates objects
// only when applied to the given namespace.
func ensureAdmissionRegistration(
	ctx context.Context, client *kubernetes.Clientset, webhookService k8stypes.NamespacedName, namespaceToCheck string,
) error {
	webhookKustomize, err := kubectl.RunKustomize(kcfg.ValidatingWebhookPath())
	if err != nil {
		return fmt.Errorf("failed to run kustomize for webhook: %w", err)
	}

	webhookConfig := &admregv1.ValidatingWebhookConfiguration{}
	if err := yaml.Unmarshal(webhookKustomize, webhookConfig); err != nil {
		return fmt.Errorf("failed to unmarshal webhook configuration: %w", err)
	}
	cert, key := certificate.GetKongSystemSelfSignedCerts()
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = cert
	}

	fmt.Println("creating webhook configuration")
	validationWebhookClient := client.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	webhookConfig, err = validationWebhookClient.Create(ctx, webhookConfig, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create validating webhook configuration: %w", err)
	}
	// t.Cleanup(func() { //nolint:contextcheck
	// 	if err := validationWebhookClient.Delete(
	// 		context.Background(), webhookConfig.Name, metav1.DeleteOptions{},
	// 	); err != nil && !apierrors.IsNotFound(err) {
	// 		return nil
	// 	}
	// })

	// Write to /tmp key and cert files for the webhook server to use
	const p = "/tmp/k8s-webhook-server/serving-certs/validating-admission-webhook/"
	// ensure path with dirs exists
	if err := os.MkdirAll(p, 0o700); err != nil {
		return fmt.Errorf("failed to create cert dir: %w", err)
	}
	if err := os.WriteFile(p+"tls.crt", cert, 0o600); err != nil {
		return fmt.Errorf("failed to write cert file: %w", err)
	}
	if err := os.WriteFile(p+"tls.key", key, 0o600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}
	return nil
}
