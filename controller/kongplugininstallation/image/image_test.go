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
		_, err := image.FetchPluginContent(context.Background(), "foo bar", nil)
		require.ErrorContains(t, err, "unexpected format of image url: could not parse reference: foo bar")
	})

	const registryUrl = "northamerica-northeast1-docker.pkg.dev/k8s-team-playground/"

	t.Run("valid image (Docker format)", func(t *testing.T) {
		plugin, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example/valid", nil,
		)
		require.NoError(t, err)
		require.Equal(t, string(plugin), "plugin-content\n")
	})

	t.Run("valid image (OCI format)", func(t *testing.T) {
		plugin, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example/valid-oci", nil,
		)
		require.NoError(t, err)
		require.Equal(t, string(plugin), "plugin-content\n")
	})

	t.Run("invalid image - to many layers", func(t *testing.T) {
		_, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example/invalid-layers", nil,
		)
		require.ErrorContains(t, err, "expected exactly one layer with plugin, found 2 layers")
	})

	t.Run("valid image from private registry", func(t *testing.T) {
		credentials := integration.GetKongPluginImageRegistryCredentialsForTests()
		if credentials == "" {
			t.Skip("skipping - no credentials provided")
		}

		credsStore, err := image.CredentialsStoreFromString(credentials)
		require.NoError(t, err)

		plugin, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example-private/valid:v1.0", credsStore,
		)
		require.NoError(t, err)
		require.Equal(t, string(plugin), "plugin-content-private\n")
	})

	t.Run("invalid image - to many layers", func(t *testing.T) {
		_, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example/invalid-layers", nil,
		)
		require.ErrorContains(t, err, "expected exactly one layer with plugin, found 2 layers")
	})

	t.Run("invalid image - invalid name of plugin inside of it", func(t *testing.T) {
		_, err := image.FetchPluginContent(
			context.Background(), registryUrl+"plugin-example/invalid-name", nil,
		)
		require.ErrorContains(t, err, `file "plugin.lua" not found in the image`)
	})
}
