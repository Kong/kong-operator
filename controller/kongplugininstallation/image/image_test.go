package image_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/kong/gateway-operator/controller/kongplugininstallation/image"
	"github.com/kong/gateway-operator/test/integration"
)

func TestCredentialsStoreFromString(t *testing.T) {
	testCases := []struct {
		name                string
		credentials         string
		expectedErrorMsg    string
		expectedCredentials func(t *testing.T, cs credentials.Store)
	}{
		{
			name:             "invalid credentials",
			credentials:      "foo",
			expectedErrorMsg: "invalid config format:",
		},
		{
			name: "valid credentials",
			// Field auth is base64 encoded "test:test".
			credentials: `
			{
 			  "auths": {
 			    "ghcr.io": {
 			      "auth": "dGVzdDp0ZXN0"
 			    }
 			  }
			}`,
			expectedCredentials: func(t *testing.T, cs credentials.Store) {
				t.Helper()
				require.NotNil(t, cs)
				c, err := cs.Get(context.Background(), "ghcr.io")
				require.NoError(t, err)
				require.Equal(t, auth.Credential{Username: "test", Password: "test", RefreshToken: "", AccessToken: ""}, c)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			credsStore, err := image.CredentialsStoreFromString(tc.credentials)
			if tc.expectedCredentials != nil {
				tc.expectedCredentials(t, credsStore)
			} else {
				require.ErrorContains(t, err, tc.expectedErrorMsg)
			}
		})
	}
}

func TestFetchPluginContent(t *testing.T) {
	t.Run("invalid image URL", func(t *testing.T) {
		_, err := image.FetchPlugin(context.Background(), "foo bar", nil)
		require.ErrorContains(t, err, "unexpected format of image url: could not parse reference: foo bar")
	})

	const registryUrl = "northamerica-northeast1-docker.pkg.dev/k8s-team-playground/"

	t.Run("valid image (Docker format)", func(t *testing.T) {
		plugin, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/valid:0.1.0", nil,
		)
		require.NoError(t, err)
		requireExpectedContent(t, plugin)
	})

	t.Run("valid image (OCI format)", func(t *testing.T) {
		plugin, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/valid-oci:0.1.0", nil,
		)
		require.NoError(t, err)
		requireExpectedContent(t, plugin)
	})

	t.Run("valid image from private registry", func(t *testing.T) {
		credentials := integration.GetKongPluginImageRegistryCredentialsForTests()
		if credentials == "" {
			t.Skip("skipping - no credentials provided")
		}

		credsStore, err := image.CredentialsStoreFromString(credentials)
		require.NoError(t, err)

		plugin, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example-private/valid:0.1.0", credsStore,
		)
		require.NoError(t, err)
		requireExpectedContentPrivate(t, plugin)
	})

	t.Run("invalid image - too many layers", func(t *testing.T) {
		_, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/invalid-layers", nil,
		)
		require.ErrorContains(t, err, "expected exactly one layer with plugin, found 2 layers")
	})

	t.Run("invalid image - invalid names of files", func(t *testing.T) {
		_, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/invalid-name", nil,
		)
		require.ErrorContains(t, err, `file "add-header.lua" is unexpected, required files are handler.lua and schema.lua`)
	})

	t.Run("invalid image - missing file", func(t *testing.T) {
		_, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/missing-file", nil,
		)
		require.ErrorContains(t, err, `not found in the image required files: schema.lua`)
	})

	// Single file - handler.lua is over 1 MiB.
	t.Run("invalid image - invalid too big plugin (size of single file)", func(t *testing.T) {
		_, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/invalid-size-one", nil,
		)
		require.ErrorContains(t, err, "plugin size exceed 1.00 MiB")
	})

	// Each file is 512 KiB so together they are 1 MiB.
	t.Run("invalid image - invalid too big plugin (size of files combined)", func(t *testing.T) {
		_, err := image.FetchPlugin(
			context.Background(), registryUrl+"plugin-example/invalid-size-combined", nil,
		)
		require.ErrorContains(t, err, "plugin size exceed 1.00 MiB")
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
