package integration

import (
	"context"
	"fmt"
	"os"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"github.com/kong/kong-operator/ingress-controller/test/consts"
	"github.com/kong/kong-operator/ingress-controller/test/helpers/certificate"
	testutils "github.com/kong/kong-operator/ingress-controller/test/util"
	"github.com/kong/kong-operator/test/helpers/kcfg"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/kubectl"
	admregv1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/yaml"
)

// ensureAdmissionRegistration registers a validating webhook for the given configuration, it validates objects
// only when applied to the given namespace.
func ensureAdmissionRegistration(
	ctx context.Context, client *kubernetes.Clientset,
) error {

	const svcPort = 5443
	webhookService := k8stypes.NamespacedName{
		Namespace: consts.ControllerNamespace,
		// Name:      fmt.Sprintf("webhook-%s", webhookName),
		Name: "gateway-operator-webhook-service",
	}
	if err := ensureWebhookService(ctx, client, webhookService); err != nil {
		return fmt.Errorf("ensure webhook service: %w", err)
	}

	webhookKustomize, err := kubectl.RunKustomize(kcfg.ValidatingWebhookPath())
	if err != nil {
		return fmt.Errorf("run kustomize for webhook: %w", err)
	}

	webhookConfig := &admregv1.ValidatingWebhookConfiguration{}
	if err := yaml.Unmarshal(webhookKustomize, webhookConfig); err != nil {
		return fmt.Errorf("unmarshal webhook kustomize output: %w", err)
	}

	cert, key := certificate.GetKongSystemSelfSignedCerts()
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.Service.Name = webhookService.Name
		webhookConfig.Webhooks[i].ClientConfig.Service.Namespace = webhookService.Namespace
		webhookConfig.Webhooks[i].ClientConfig.CABundle = cert
		// webhookConfig.Webhooks[i].NamespaceSelector = &metav1.LabelSelector{
		// 	MatchLabels: map[string]string{
		// 		"kubernetes.io/metadata.name": namespaceToCheck,
		// 	},
		// }
	}

	// Write to /tmp key and cert files for the webhook server to use
	const p = "/tmp/k8s-webhook-server/serving-certs/validating-admission-webhook/"
	// ensure path with dirs exists
	if err := os.MkdirAll(p, 0o700); err != nil {
		return fmt.Errorf("mkdir all %s: %w", p, err)
	}
	if err := os.WriteFile(p+"tls.crt", cert, 0o600); err != nil {
		return fmt.Errorf("write tls.crt: %w", err)
	}
	if err := os.WriteFile(p+"tls.key", key, 0o600); err != nil {
		return fmt.Errorf("write tls.key: %w", err)
	}

	fmt.Println("creating webhook configuration")
	validationWebhookClient := client.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	webhookConfig, err = validationWebhookClient.Create(ctx, webhookConfig, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create validating webhook configuration: %w", err)
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
	validationsService, err := client.CoreV1().Services(nn.Namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: nn.Name,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "default",
					Port:       5443,
					TargetPort: intstr.FromInt(testutils.AdmissionWebhookListenPort),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create service %s/%s: %w", nn.Namespace, nn.Name, err)
	}
	_ = validationsService

	fmt.Println("creating webhook endpoints")
	endpoints, err := client.DiscoveryV1().EndpointSlices(nn.Namespace).Create(ctx, &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-1", nn.Name),
			Labels: map[string]string{
				discoveryv1.LabelServiceName: nn.Name,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{testutils.GetAdmissionWebhookListenHost()},
			},
		},
		Ports: NewEndpointPort(testutils.AdmissionWebhookListenPort).WithName("default").WithProtocol(corev1.ProtocolTCP).IntoSlice(),
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

// EndpointPortBuilder is a builder for discovery v1 EndpointPort.
// Primarily used for testing.
type EndpointPortBuilder struct {
	ep discoveryv1.EndpointPort
}

func NewEndpointPort(port int32) *EndpointPortBuilder {
	return &EndpointPortBuilder{
		ep: discoveryv1.EndpointPort{
			Port: lo.ToPtr(port),
		},
	}
}

// WithProtocol sets the protocol on the endpoint port.
func (b *EndpointPortBuilder) WithProtocol(proto corev1.Protocol) *EndpointPortBuilder {
	b.ep.Protocol = lo.ToPtr(proto)
	return b
}

// WithName sets the name on the endpoint port.
func (b *EndpointPortBuilder) WithName(name string) *EndpointPortBuilder {
	b.ep.Name = lo.ToPtr(name)
	return b
}

// Build returns the configured EndpointPort.
func (b *EndpointPortBuilder) Build() discoveryv1.EndpointPort {
	return b.ep
}

// IntoSlice returns the configured EndpointPort in a slice.
func (b *EndpointPortBuilder) IntoSlice() []discoveryv1.EndpointPort {
	return []discoveryv1.EndpointPort{b.ep}
}
