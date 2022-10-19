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
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesclient "k8s.io/client-go/kubernetes"

	testutils "github.com/kong/gateway-operator/internal/utils/test"
)

// TODO https://github.com/Kong/kubernetes-testing-framework/issues/302
// we have this in both integration and e2e pkgs, and also in the controller integration pkg
// they should be standardized

// Setup is a helper function for tests which conveniently creates a cluster
// cleaner (to clean up test resources automatically after the test finishes)
// and creates a new namespace for the test to use. It also enables parallel
// testing.
func setup(t *testing.T, ctx context.Context, env environments.Environment, clients testutils.K8sClients) (*corev1.Namespace, *clusters.Cleaner) {
	t.Log("performing test setup")
	t.Parallel()
	cleaner := clusters.NewCleaner(env.Cluster())

	t.Log("creating a testing namespace")
	namespace, err := clients.K8sClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.AddNamespace(namespace)

	return namespace, cleaner
}

// Expect404WithNoRoute is used to check whether a given http response is (specifically) a Kong 404.
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

	switch service.Spec.Type {
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

// CreateValidatingWebhook creates validating webhook for gateway operator.
func createValidatingWebhook(ctx context.Context, k8sClient *kubernetesclient.Clientset, webhookURL string, caPath string) error {
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

// GetFirstNonLoopbackIP returns the first found non-loopback IPv4 ip of local interfaces.
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

// GetEnvValueByName returns the corresponding value of LAST item with given name.
// returns empty string if the name not appeared.
func getEnvValueByName(envs []corev1.EnvVar, name string) string {
	value := ""
	for _, env := range envs {
		if env.Name == name {
			value = env.Value
		}
	}
	return value
}

// setEnvValueByName sets the EnvVar in slice with the provided name and value.
func setEnvValueByName(envs []corev1.EnvVar, name string, value string) []corev1.EnvVar {
	for _, env := range envs {
		if env.Name == name {
			env.Value = value
			return envs
		}
	}
	return append(envs, corev1.EnvVar{
		Name:  name,
		Value: value,
	})
}

// GetEnvValueFromByName returns the corresponding ValueFrom pointer of LAST item with given name.
// returns nil if the name not appeared.
func getEnvValueFromByName(envs []corev1.EnvVar, name string) *corev1.EnvVarSource {
	var valueFrom *corev1.EnvVarSource
	for _, env := range envs {
		if env.Name == name {
			valueFrom = env.ValueFrom
		}
	}

	return valueFrom
}
