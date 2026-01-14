package webhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/avast/retry-go/v4"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/kubectl"
	"github.com/samber/lo"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/ingress-controller/pkg/validation/consts"
	"github.com/kong/kong-operator/test/helpers/certificate"
	"github.com/kong/kong-operator/test/helpers/kcfg"
)

// EnsureValidatingWebhookRegistration registers a validating webhook and configures connectivity for
// the webhook defined in kubernetes manifests. When successful, it returns a function to check connectivity
// to the webhook (should be called after bootstrapping the controller) and a cleanup function to remove
// the webhook registration and associated resources. When an error is returned, no resources are created.
func EnsureValidatingWebhookRegistration(
	ctx context.Context, client *kubernetes.Clientset, controllerNamespace string,
) (checkConnectivityToWebhook func() error, cleanup func(), err error) {
	webhookKustomize, err := kubectl.RunKustomize(kcfg.ValidatingWebhookPath())
	if err != nil {
		return nil, nil, fmt.Errorf("run kustomize for webhook: %w", err)
	}
	webhookConfig := &admregv1.ValidatingWebhookConfiguration{}
	if err := yaml.Unmarshal(webhookKustomize, webhookConfig); err != nil {
		return nil, nil, fmt.Errorf("unmarshal webhook kustomize output: %w", err)
	}
	if len(webhookConfig.Webhooks) == 0 || webhookConfig.Webhooks[0].ClientConfig.Service == nil {
		return nil, nil, fmt.Errorf("no webhook Service configuration found")
	}
	webhookService := k8stypes.NamespacedName{
		Name:      webhookConfig.Webhooks[0].ClientConfig.Service.Name,
		Namespace: webhookConfig.Webhooks[0].ClientConfig.Service.Namespace,
	}

	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName(fmt.Sprintf("*.%s.svc", controllerNamespace)),
		certificate.WithDNSNames(fmt.Sprintf("*.%s.svc", controllerNamespace)),
	)
	for i := range webhookConfig.Webhooks {
		webhookConfig.Webhooks[i].ClientConfig.CABundle = cert
	}

	fmt.Printf("INFO: ensuring certificates for webhook in: %s\n", consts.DefaultAdmissionWebhookBasePath)
	if err := os.MkdirAll(consts.DefaultAdmissionWebhookBasePath, 0o700); err != nil {
		return nil, nil, fmt.Errorf("mkdir path %s: %w", consts.DefaultAdmissionWebhookBasePath, err)
	}
	if err := os.WriteFile(consts.DefaultAdmissionWebhookCertPath, cert, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write file: %w", err)
	}
	if err := os.WriteFile(consts.DefaultAdmissionWebhookKeyPath, key, 0o600); err != nil {
		return nil, nil, fmt.Errorf("write file: %w", err)
	}
	cleanupCertFiles := func() {
		if err := os.RemoveAll(consts.DefaultAdmissionWebhookBasePath); err != nil {
			fmt.Printf("failed to cleanup webhook certs path %s: %v\n", consts.DefaultAdmissionWebhookBasePath, err)
		}
	}

	cleanupSvc, err := ensureWebhookService(ctx, client, webhookService)
	if err != nil {
		cleanupCertFiles()
		return nil, nil, fmt.Errorf("ensure webhook service: %w", err)
	}

	fmt.Println("INFO: creating webhook configuration")
	validationWebhookClient := client.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	if _, err = validationWebhookClient.Create(ctx, webhookConfig, metav1.CreateOptions{}); err != nil {
		cleanupCertFiles()
		cleanupSvc()
		return nil, nil, fmt.Errorf("create validating webhook configuration: %w", err)
	}
	cleanupAdmissionCfg := func() {
		ctx := context.Background()
		if err := validationWebhookClient.Delete(ctx, webhookConfig.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("failed to delete validating webhook configuration %s: %v", webhookConfig.Name, err)
		}
	}

	checkConnectivityToWebhook = func() error {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(cert) {
			return fmt.Errorf("failed to append CA from PEM to cert pool")
		}
		cl := http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					RootCAs:    certPool,
				},
			},
		}
		return retry.Do(
			func() error {
				// Body and response are not relevant here, the goal is to just check connectivity.
				resp, err := cl.Get(fmt.Sprintf("https://%s.%s.svc:%d", webhookService.Name, webhookService.Namespace, consts.WebhookPort))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			},
			retry.OnRetry(
				func(n uint, err error) {
					fmt.Printf("WARNING: try to connect to validating webhook Service attempt %d/10 - error: %s\n", n+1, err)
				},
			),
			retry.LastErrorOnly(true),
			retry.Attempts(10),
		)
	}

	cleanup = func() {
		cleanupCertFiles()
		cleanupSvc()
		cleanupAdmissionCfg()
	}

	return checkConnectivityToWebhook, cleanup, nil
}

func ensureWebhookService(
	ctx context.Context, client *kubernetes.Clientset, nn k8stypes.NamespacedName,
) (cleanup func(), err error) {
	fmt.Printf("INFO: creating webhook service: %s\n", nn)
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
		return nil, fmt.Errorf("create service %s/%s: %w", nn.Namespace, nn.Name, err)
	}

	fmt.Println("INFO: creating webhook endpoints")
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
					Addresses: []string{getLocalOperatorListenHost()},
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
		// Attempt cleanup of service if endpoints creation failed.
		if delErr := client.CoreV1().Services(nn.Namespace).Delete(context.Background(), validationsService.Name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
			fmt.Printf("WARN: failed to cleanup service after endpoints create failure: %v\n", delErr)
		}
		return nil, fmt.Errorf("create EndpointSlice %s/%s: %w", nn.Namespace, nn.Name, err)
	}

	cleanup = func() {
		ctx := context.Background()
		if err := client.CoreV1().Services(nn.Namespace).Delete(ctx, validationsService.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("WARN: failed to delete service %s/%s: %v", nn.Namespace, validationsService.Name, err)
		}
		if err := client.DiscoveryV1().EndpointSlices(nn.Namespace).Delete(ctx, endpoints.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			fmt.Printf("WARN: failed to delete EndpointSlice %s/%s: %v", nn.Namespace, endpoints.Name, err)
		}
	}

	return cleanup, nil
}
