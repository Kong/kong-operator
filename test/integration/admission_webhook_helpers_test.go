package integration

import (
	"context"
	"fmt"
	"os"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/kubectl"
	"github.com/samber/lo"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/ingress-controller/pkg/validation/consts"
	"github.com/kong/kong-operator/ingress-controller/test/helpers/certificate"
	"github.com/kong/kong-operator/test/helpers/kcfg"
	"github.com/kong/kong-operator/test/helpers/webhook"
)

// ensureAdmissionRegistration registers a validating webhook for the given configuration, it validates objects
// only when applied to the given namespace.
func ensureAdmissionRegistration(
	ctx context.Context, client *kubernetes.Clientset,
) error {
	webhookKustomize, err := kubectl.RunKustomize(kcfg.ValidatingWebhookPath())
	if err != nil {
		return fmt.Errorf("run kustomize for webhook: %w", err)
	}
	webhookConfig := &admregv1.ValidatingWebhookConfiguration{}
	if err := yaml.Unmarshal(webhookKustomize, webhookConfig); err != nil {
		return fmt.Errorf("unmarshal webhook kustomize output: %w", err)
	}
	if len(webhookConfig.Webhooks) == 0 || webhookConfig.Webhooks[0].ClientConfig.Service == nil {
		return fmt.Errorf("no webhook Service configuration found")
	}
	webhookService := k8stypes.NamespacedName{
		Name:      webhookConfig.Webhooks[0].ClientConfig.Service.Name,
		Namespace: webhookConfig.Webhooks[0].ClientConfig.Service.Namespace,
	}

	if err := ensureWebhookService(ctx, client, webhookService); err != nil {
		return fmt.Errorf("ensure webhook service: %w", err)
	}

	cert, key := certificate.GetKongSystemSelfSignedCerts()
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = cert
	}

	fmt.Println("creating webhook configuration")
	validationWebhookClient := client.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	if _, err = validationWebhookClient.Create(ctx, webhookConfig, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create validating webhook configuration: %w", err)
	}

	if err := os.MkdirAll(consts.DefaultAdmissionWebhookBasePath, 0o700); err != nil {
		return fmt.Errorf("mkdir path %s: %w", consts.DefaultAdmissionWebhookBasePath, err)
	}
	if err := os.WriteFile(consts.DefaultAdmissionWebhookCertPath, cert, 0o600); err != nil {
		return fmt.Errorf("write tls.crt: %w", err)
	}
	if err := os.WriteFile(consts.DefaultAdmissionWebhookKeyPath, key, 0o600); err != nil {
		return fmt.Errorf("write tls.key: %w", err)
	}

	// t.Cleanup(func() { //nolint:contextcheck
	// 	if err := validationWebhookClient.Delete(
	// 		context.Background(), webhookConfig.Name, metav1.DeleteOptions{},
	// 	); err != nil && !apierrors.IsNotFound(err) {
	// 		fmt.Printf("failed to delete validating webhook configuration %s: %v", webhookConfig.Name, err)
	// 	}
	// })

	return nil
}

func ensureWebhookService(ctx context.Context, client *kubernetes.Clientset, nn k8stypes.NamespacedName) error {
	fmt.Printf("creating webhook service: %s\n", nn)
	validationsService, err := client.CoreV1().Services(nn.Namespace).Create(
		ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: nn.Name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{
						Name:       "default",
						Port:       consts.WebhookPort,
						TargetPort: intstr.FromInt(consts.WebhookPort),
					},
				},
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create service %s/%s: %w", nn.Namespace, nn.Name, err)
	}
	_ = validationsService

	fmt.Println("creating webhook endpoints")
	endpoints, err := client.DiscoveryV1().EndpointSlices(nn.Namespace).Create(
		ctx,
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-1", nn.Name),
				Labels: map[string]string{
					discoveryv1.LabelServiceName: nn.Name,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{webhook.GetAdmissionWebhookListenHost()},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name:     lo.ToPtr("default"),
					Port:     lo.ToPtr(int32(consts.WebhookPort)),
					Protocol: lo.ToPtr(corev1.ProtocolTCP),
				},
			},
		}, metav1.CreateOptions{})
	if err != nil {
		// attempt cleanup of service if endpoints creation failed
		// if delErr := client.CoreV1().Services(nn.Namespace).Delete(context.Background(), validationsService.Name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
		// 	t.Logf("failed to cleanup service after endpoints create failure: %v", delErr)
		// }
		return fmt.Errorf("create endpointslice %s/%s: %w", nn.Namespace, nn.Name, err)
	}
	_ = endpoints

	// t.Cleanup(func() { //nolint:contextcheck
	// 	ctx := context.Background()
	// 	if err := client.CoreV1().Services(nn.Namespace).Delete(ctx, validationsService.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
	// 		fmt.Printf("failed to delete service %s/%s: %v", nn.Namespace, validationsService.Name, err)
	// 	}
	// 	if err := client.DiscoveryV1().EndpointSlices(nn.Namespace).Delete(ctx, endpoints.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
	// 		fmt.Printf("failed to delete endpointslice %s/%s: %v", nn.Namespace, endpoints.Name, err)
	// 	}
	// })

	return nil
}
