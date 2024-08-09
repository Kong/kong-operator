package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetesclient "k8s.io/client-go/kubernetes"

	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/test/helpers"
)

// Expect404WithNoRouteFunc is used to check whether a given URL responds
// with 404 and a standard Kong no route message.
func Expect404WithNoRouteFunc(t *testing.T, ctx context.Context, url string) func() bool {
	t.Helper()

	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)

	return func() bool {
		t.Logf("verifying connectivity to the dataplane %v", url)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Logf("failed creating request for %s: %v", url, err)
			return false
		}
		resp, err := httpClient.Do(req)
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

func URLForService(ctx context.Context, cluster clusters.Cluster, nsn types.NamespacedName, port int) (*url.URL, error) {
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
func CreateValidatingWebhook(ctx context.Context, k8sClient *kubernetesclient.Clientset, webhookURL string, ca, cert, key []byte) error {
	if _, err := k8sClient.CoreV1().Secrets("kong-system").Create(
		ctx,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: consts.WebhookCertificateConfigSecretName,
			},
			Data: map[string][]byte{
				consts.CAFieldSecret:   ca,
				consts.CertFieldSecret: cert,
				consts.KeyFieldSecret:  key,
			},
		},
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	validationWebhook := resources.NewValidatingWebhookConfigurationBuilder(consts.WebhookName).
		WithScopeAllNamespaces().
		WithClientConfigURL(webhookURL).
		WithCABundle(ca).
		Build()
	if _, err := k8sClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Create(
		ctx,
		validationWebhook,
		metav1.CreateOptions{},
	); err != nil {
		return err
	}
	return nil
}

// GetEnvValueByName returns the corresponding value of LAST item with given name.
// returns empty string if the name not appeared.
func GetEnvValueByName(envs []corev1.EnvVar, name string) string {
	value := ""
	for _, env := range envs {
		if env.Name == name {
			value = env.Value
		}
	}
	return value
}

// SetEnvValueByName sets the EnvVar in slice with the provided name and value.
func SetEnvValueByName(envs []corev1.EnvVar, name string, value string) []corev1.EnvVar {
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
func GetEnvValueFromByName(envs []corev1.EnvVar, name string) *corev1.EnvVarSource {
	var valueFrom *corev1.EnvVarSource
	for _, env := range envs {
		if env.Name == name {
			valueFrom = env.ValueFrom
		}
	}

	return valueFrom
}

func GetVolumeByName(volumes []corev1.Volume, name string) *corev1.Volume {
	for _, v := range volumes {
		if v.Name == name {
			return v.DeepCopy()
		}
	}
	return nil
}

func GetVolumeMountsByVolumeName(volumeMounts []corev1.VolumeMount, name string) []corev1.VolumeMount {
	ret := make([]corev1.VolumeMount, 0)
	for _, m := range volumeMounts {
		if m.Name == name {
			ret = append(ret, m)
		}
	}
	return ret
}

func GetKongPluginImageRegistryCredentialsForTests() string {
	return os.Getenv("KONG_PLUGIN_IMAGE_REGISTRY_CREDENTIALS")
}
