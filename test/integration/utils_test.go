//go:build integration_tests
// +build integration_tests

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesclient "k8s.io/client-go/kubernetes"
)

// expect404WithNoRouteFunc is used to check whether a given URL responds
// with 404 and a standard Kong no route message.
func expect404WithNoRouteFunc(t *testing.T, ctx context.Context, url string) func() bool {
	t.Helper()

	return func() bool {
		t.Logf("verifying connectivity to the dataplane %v", url)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			t.Logf("failed creating request for %s: %v", url, err)
			return false
		}
		resp, err := httpc.Do(req)
		if err != nil {
			t.Logf("failed issuing HTTP GET for %s: %v", url, err)
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Logf("expected 404 got %d from HTTP GET for %s: %v", resp.StatusCode, url, err)
			return false
		}
		var proxyResponse struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&proxyResponse); err != nil {
			t.Logf("failed decoding HTTP GET response from %s: %v", url, err)
			return false
		}

		const expected = "no Route matched with those values"
		if expected != proxyResponse.Message {
			t.Logf("expected %s got in HTTP GET response from %s", expected, url)
			return false
		}
		return true
	}
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

func getVolumeByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for _, v := range volumes {
		if v.Name == name {
			return v.DeepCopy()
		}
	}
	return nil
}

func getVolumeMountsByVolumeName(volumeMounts []corev1.VolumeMount, name string) []corev1.VolumeMount {
	ret := make([]corev1.VolumeMount, 0)
	for _, m := range volumeMounts {
		if m.Name == name {
			ret = append(ret, m)
		}
	}
	return ret
}
