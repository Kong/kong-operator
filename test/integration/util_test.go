//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultHTTPPort = 80
)

// setup is a helper function for tests which conveniently creates a cluster
// cleaner (to clean up test resources automatically after the test finishes)
// and creates a new namespace for the test to use. It also enables parallel
// testing.
func setup(t *testing.T) (*corev1.Namespace, *clusters.Cleaner) {
	t.Log("performing test setup")
	t.Parallel()
	cleaner := clusters.NewCleaner(env.Cluster())

	t.Log("creating a testing namespace")
	namespace, err := k8sClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.AddNamespace(namespace)

	return namespace, cleaner
}

// expect404WithNoRoute is used to check whether a given http response is (specifically) a Kong 404.
func expect404WithNoRoute(t *testing.T, proxyURL string, resp *http.Response) bool {
	if resp.StatusCode == http.StatusNotFound {
		// once the route is torn down and returning 404's, ensure that we got the expected response body back from Kong
		// Expected: {"message":"no Route matched with those values"}
		b := new(bytes.Buffer)
		_, err := b.ReadFrom(resp.Body)
		require.NoError(t, err)
		body := struct {
			Message string `json:"message"`
		}{}
		if err := json.Unmarshal(b.Bytes(), &body); err != nil {
			t.Logf("WARNING: error decoding JSON from proxy while waiting for %s: %v", proxyURL, err)
			return false
		}
		return body.Message == "no Route matched with those values"
	}
	return false
}

func urlForService(ctx context.Context, cluster clusters.Cluster, nsn types.NamespacedName, port int) (*url.URL, error) {
	service, err := cluster.Client().CoreV1().Services(nsn.Namespace).Get(ctx, nsn.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	switch service.Spec.Type { //nolint:exhaustive
	case corev1.ServiceTypeLoadBalancer:
		if len(service.Status.LoadBalancer.Ingress) == 1 {
			return url.Parse(fmt.Sprintf("http://%s:%d", service.Status.LoadBalancer.Ingress[0].IP, port))
		}
	default:
		if service.Spec.ClusterIP != "" {
			return url.Parse(fmt.Sprintf("http://%s:%d", service.Spec.ClusterIP, port))
		}
	}

	return nil, fmt.Errorf("service %s has not yet been provisoned", service.Name)
}

// createValidatingWebhook creates validating webhook for gateway operator.
func createValidatingWebhook(ctx context.Context, k8sClient *kubernetes.Clientset, webhookURL string, caPath string) error {
	sideEffect := admissionregistrationv1.SideEffectClassNone
	caFile, err := os.Open(caPath)
	if err != nil {
		return err
	}
	caContent, err := io.ReadAll(caFile)
	if err != nil {
		return err
	}

	_, err = k8sClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(ctx,
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gateway-operator-validating-webhook",
			},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{
					Name: "gateway-operator-validation.konghq.com",
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						URL:      &webhookURL,
						CABundle: caContent,
					},
					Rules: []admissionregistrationv1.RuleWithOperations{
						{
							Operations: []admissionregistrationv1.OperationType{
								"CREATE",
								"UPDATE",
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"gateway-operator.konghq.com"},
								APIVersions: []string{"v1alpha1"},
								Resources:   []string{"controlplanes", "dataplanes"},
							},
						},
					},
					SideEffects:             &sideEffect,
					AdmissionReviewVersions: []string{"v1", "v1beta1"},
				},
			},
		},
		metav1.CreateOptions{})
	return err
}

// getFirstNonLoopbackIP returns the first found non-loopback IPv4 ip of local interfaces.
func getFirstNonLoopbackIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip.To4() != nil && !ip.IsLoopback() {
				return ip.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no available IPs")
}
