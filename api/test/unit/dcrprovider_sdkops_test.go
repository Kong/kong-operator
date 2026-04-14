package test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
)

func TestDcrProviderSDKOpsConversions(t *testing.T) {
	const rawSpec = `{
		"type": "Http",
		"http": {
			"name": "provider-name",
			"display_name": "Provider Name",
			"issuer": "https://issuer.example.com",
			"labels": {
				"environment": "test"
			},
			"provider_type": "http",
			"dcr_config": {
				"dcr_base_url": "https://dcr.example.com",
				"api_key": "secret",
				"disable_event_hooks": "Enabled",
				"disable_refresh_secret": "Disabled",
				"allow_multiple_credentials": "Enabled"
			}
		}
	}`

	var spec xkonnectv1alpha1.DcrProviderAPISpec
	require.NoError(t, json.Unmarshal([]byte(rawSpec), &spec))

	t.Run("create", func(t *testing.T) {
		req, err := spec.ToCreateDcrProviderRequest()
		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.CreateDcrProviderRequestHTTP)

		httpReq := req.CreateDcrProviderRequestHTTP
		require.Equal(t, "provider-name", httpReq.Name)
		require.Equal(t, "http", string(httpReq.ProviderType))
		require.NotNil(t, httpReq.DisplayName)
		require.Equal(t, "Provider Name", *httpReq.DisplayName)
		require.Equal(t, "https://issuer.example.com", httpReq.Issuer)
		require.Equal(t, map[string]string{"environment": "test"}, httpReq.Labels)
		require.NotNil(t, httpReq.DcrConfig.DisableEventHooks)
		require.True(t, *httpReq.DcrConfig.DisableEventHooks)
		require.NotNil(t, httpReq.DcrConfig.DisableRefreshSecret)
		require.False(t, *httpReq.DcrConfig.DisableRefreshSecret)
		require.NotNil(t, httpReq.DcrConfig.AllowMultipleCredentials)
		require.True(t, *httpReq.DcrConfig.AllowMultipleCredentials)
	})

	t.Run("update", func(t *testing.T) {
		req, err := spec.ToUpdateDcrProviderRequest()
		require.NoError(t, err)
		require.NotNil(t, req)
		require.NotNil(t, req.Name)
		require.Equal(t, "provider-name", *req.Name)
		require.NotNil(t, req.DisplayName)
		require.Equal(t, "Provider Name", *req.DisplayName)
		require.NotNil(t, req.Issuer)
		require.Equal(t, "https://issuer.example.com", *req.Issuer)

		data, err := json.Marshal(req)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(data, &payload))
		require.Equal(t, "provider-name", payload["name"])
		require.Equal(t, "Provider Name", payload["display_name"])
		require.Equal(t, "https://issuer.example.com", payload["issuer"])
		require.Equal(t, map[string]any{"environment": "test"}, payload["labels"])
		require.Equal(t, map[string]any{
			"api_key":                    "secret",
			"allow_multiple_credentials": true,
			"dcr_base_url":               "https://dcr.example.com",
			"disable_event_hooks":        true,
			"disable_refresh_secret":     false,
		}, payload["dcr_config"])
	})
}
