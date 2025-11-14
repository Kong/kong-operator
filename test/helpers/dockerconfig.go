package helpers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// CreateKindConfigWithDockerCredentialsBasedOnEnvVars creates a kind config with docker credentials.
func CreateKindConfigWithDockerCredentialsBasedOnEnvVars(
	ctx context.Context,
) (string, error) {
	const (
		kindConfigTemplate = `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  - containerPath: /var/lib/kubelet/config.json
    hostPath: %s`
	)

	dockerHubUser := os.Getenv("DOCKERHUB_PULL_USERNAME")
	dockerHubToken := os.Getenv("DOCKERHUB_PULL_TOKEN")
	if dockerHubUser == "" || dockerHubToken == "" {
		return "", MissingDockerHubEnvVarError{}
	}

	regCfgMgr := NewDockerRegistryConfigManager()
	if err := regCfgMgr.Add("https://index.docker.io/v1/", dockerHubUser, dockerHubToken); err != nil {
		return "", err
	}

	cfgEncoded, err := regCfgMgr.EncodeForRegcred()
	if err != nil {
		return "", err
	}

	dockerConfigDir, err := os.MkdirTemp("", "dockerconfig")
	if err != nil {
		return "", err
	}
	dockerConfigFileName := filepath.Join(dockerConfigDir, "config.json")
	if err := os.WriteFile(dockerConfigFileName, cfgEncoded, 0o777); err != nil { //nolint:gosec
		return "", err
	}

	kindConfigDir, err := os.MkdirTemp("", "kindconfig")
	if err != nil {
		return "", err
	}

	kindConfigFileName := filepath.Join(kindConfigDir, "config.yaml")
	if err := os.WriteFile(kindConfigFileName, []byte(fmt.Sprintf(kindConfigTemplate, dockerConfigFileName)), 0o777); err != nil { //nolint:gosec
		return "", err
	}

	return kindConfigFileName, err
}
