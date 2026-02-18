package image_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	orascreds "oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/kong/kong-operator/v2/controller/kongplugininstallation/image"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestFetchPluginContent(t *testing.T) {
	t.Log("This test accesses container registries on public internet")

	t.Run("invalid image URL", func(t *testing.T) {
		_, err := image.FetchPlugin(t.Context(), "foo bar", nil)
		require.ErrorContains(t, err, "unexpected format of image url: could not parse reference: foo bar")
	})

	// Learn more how images were build and pushed to the registry in hack/plugin-images/README.md.
	const registryURL = "northamerica-northeast1-docker.pkg.dev/k8s-team-playground/"

	// Source: hack/plugin-images/myheader.Dockerfile.
	t.Run("valid image (Docker format)", func(t *testing.T) {
		plugin, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/valid:0.1.0", nil,
		)
		require.NoError(t, err)
		requireExpectedContent(t, plugin)
	})

	// Source: hack/plugin-images/myheader.Dockerfile, but with different build tool.
	// Instead of Docker Podman or Buildah is used to build the image.
	t.Run("valid image (OCI format)", func(t *testing.T) {
		plugin, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/valid-oci:0.1.0", nil,
		)
		require.NoError(t, err)
		requireExpectedContent(t, plugin)
	})

	// Source: hack/plugin-images/myheader.Dockerfile.
	t.Run("valid image from private registry", func(t *testing.T) {
		credentials := integration.GetKongPluginImageRegistryCredentialsForTests()
		if credentials == "" {
			t.Skip("skipping - no credentials provided")
		}

		credsStore, err := orascreds.NewMemoryStoreFromDockerConfig([]byte(credentials))
		require.NoError(t, err)

		plugin, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example-private/valid:0.1.0", credsStore,
		)
		require.NoError(t, err)
		requireExpectedContentPrivate(t, plugin)
	})

	// Source: hack/plugin-images/invalid-layers.Dockerfile.
	t.Run("invalid image - too many layers", func(t *testing.T) {
		_, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/invalid-layers", nil,
		)
		require.ErrorContains(t, err, "expected exactly one layer with plugin, found 2 layers")
	})

	// Source: hack/plugin-images/invalid-name.Dockerfile.
	t.Run("invalid image - invalid names of files", func(t *testing.T) {
		_, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/invalid-name", nil,
		)
		require.ErrorContains(t, err, `file "add-header.lua" is unexpected, required files are handler.lua and schema.lua`)
	})

	// Source: hack/plugin-images/missing-file.Dockerfile.
	t.Run("invalid image - missing file", func(t *testing.T) {
		_, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/missing-file", nil,
		)
		require.ErrorContains(t, err, `required files not found in the image: schema.lua`)
	})

	// Source: hack/plugin-images/invalid-size-one.Dockerfile.
	t.Run("invalid image - invalid too big plugin (size of single file)", func(t *testing.T) {
		_, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/invalid-size-one", nil,
		)
		require.ErrorContains(t, err, "plugin size limit of 1.00 MiB exceeded")
	})

	// Source: hack/plugin-images/invalid-size-combined.Dockerfile.
	t.Run("invalid image - invalid too big plugin (size of files combined)", func(t *testing.T) {
		_, err := image.FetchPlugin(
			t.Context(), registryURL+"plugin-example/invalid-size-combined", nil,
		)
		require.ErrorContains(t, err, "plugin size limit of 1.00 MiB exceeded")
	})
}

func requireExpectedContent(t *testing.T, actual map[string]string) {
	t.Helper()
	require.Len(t, actual, 2)
	require.Equal(t, map[string]string{
		"handler.lua": "handler-content\n",
		"schema.lua":  "schema-content\n",
	}, actual)
}

func requireExpectedContentPrivate(t *testing.T, actual map[string]string) {
	t.Helper()
	require.Len(t, actual, 2)
	require.Equal(t, map[string]string{
		"handler.lua": "handler-content-private\n",
		"schema.lua":  "schema-content-private\n",
	}, actual)
}
