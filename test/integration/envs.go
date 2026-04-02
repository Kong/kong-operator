package integration

import "os"

// GetKongPluginImageRegistryCredentialsForTests returns the credentials for the image registry with plugins for tests.
// The expected format is the same as ~/.docker/config.json, see
// https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#log-in-to-docker-hub
func GetKongPluginImageRegistryCredentialsForTests() string {
	return os.Getenv("KONG_PLUGIN_IMAGE_REGISTRY_CREDENTIALS")
}
