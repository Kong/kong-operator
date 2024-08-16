package helpers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgocorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type dockerRegistryConfig struct {
	Auths map[string]DockerRegistryAuth `json:"auths"`
}

// DockerRegistryAuth represents the auth field in the docker registry config.
type DockerRegistryAuth struct {
	Auth string `json:"auth"`
}

// DockerRegistryConfigManager is a helper to manage the docker registry config
// for a k8s secret of type kubernetes.io/dockerconfigjson.
type DockerRegistryConfigManager struct {
	config dockerRegistryConfig
}

// NewDockerRegistryConfigManager creates a new DockerRegistryConfigManager.
func NewDockerRegistryConfigManager() *DockerRegistryConfigManager {
	return &DockerRegistryConfigManager{
		config: dockerRegistryConfig{
			Auths: map[string]DockerRegistryAuth{},
		},
	}
}

// Add adds new registry credentials to the config.
func (c *DockerRegistryConfigManager) Add(
	registry string,
	username string,
	token string,
) error {
	auth := fmt.Sprintf("%s:%s", username, token)
	authEncoded := base64.StdEncoding.EncodeToString([]byte(auth))
	c.config.Auths[registry] = DockerRegistryAuth{
		Auth: authEncoded,
	}
	return nil
}

// EncodeForRegcred encodes the config for a k8s secret of type kubernetes.io/dockerconfigjson.
func (c *DockerRegistryConfigManager) EncodeForRegcred() ([]byte, error) {
	b, err := json.Marshal(c.config)
	if err != nil {
		return nil, err
	}
	// out := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	// base64.StdEncoding.Encode(out, b)
	return b, nil
}

// MissingDockerHubEnvVarError is an error type for missing required env vars for DockerHub pull secret.
type MissingDockerHubEnvVarError struct{}

// Error returns the error message.
func (e MissingDockerHubEnvVarError) Error() string {
	return "missing required env vars for DockerHub pull secret"
}

// CreateDockerSecretBasedOnEnvVars creates a k8s secret of type kubernetes.io/dockerconfigjson.
// The provided client controls in which namespace to create the secret.
func CreateDockerSecretBasedOnEnvVars(
	ctx context.Context, cl clientgocorev1.SecretInterface,
) (*corev1.Secret, error) {
	dockerHubUser := os.Getenv("DOCKERHUB_PULL_USERNAME")
	dockerHubToken := os.Getenv("DOCKERHUB_PULL_TOKEN")
	if dockerHubUser == "" || dockerHubToken == "" {
		return nil, MissingDockerHubEnvVarError{}
	}

	if secret, err := cl.Get(ctx, "regcred", metav1.GetOptions{}); err == nil {
		return secret, nil
	}

	regCfgMgr := NewDockerRegistryConfigManager()
	if err := regCfgMgr.Add("https://index.docker.io/v1/", dockerHubUser, dockerHubToken); err != nil {
		return nil, err
	}
	cfgEncoded, err := regCfgMgr.EncodeForRegcred()
	if err != nil {
		return nil, err
	}
	dockerHubTokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "regcred",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		StringData: map[string]string{
			corev1.DockerConfigJsonKey: string(cfgEncoded),
		},
	}

	dockerHubTokenSecret, err = cl.Create(ctx, dockerHubTokenSecret, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return dockerHubTokenSecret, nil
}
