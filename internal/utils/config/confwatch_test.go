package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestEnvVarNameToKongConfKey(t *testing.T) {
	testCases := []struct {
		name     string
		envVar   string
		expected string
	}{
		{
			name:     "proxy listen",
			envVar:   "KONG_PROXY_LISTEN",
			expected: "proxy_listen",
		},
		{
			name:     "stream listen",
			envVar:   "KONG_STREAM_LISTEN",
			expected: "stream_listen",
		},
		{
			name:     "port maps",
			envVar:   "KONG_PORT_MAPS",
			expected: "port_maps",
		},
		{
			name:     "no KONG_ prefix is left alone, only lowercased",
			envVar:   "PORT_MAPS",
			expected: "port_maps",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, EnvVarNameToKongConfKey(tc.envVar))
		})
	}
}

func TestExtractConfWatchEnvVars(t *testing.T) {
	testCases := []struct {
		name              string
		env               []corev1.EnvVar
		expectedManaged   map[string]string
		expectedRemaining []string
	}{
		{
			name: "listen settings are extracted, everything else stays",
			env: []corev1.EnvVar{
				{Name: "KONG_DATABASE", Value: "off"},
				{Name: "KONG_PROXY_LISTEN", Value: "0.0.0.0:8000 http2"},
				{Name: "KONG_ADMIN_LISTEN", Value: "0.0.0.0:8444 ssl"},
				{Name: "KONG_STREAM_LISTEN", Value: "0.0.0.0:8899"},
				{Name: "KONG_PORT_MAPS", Value: "80:8000, 443:8443"},
			},
			expectedManaged: map[string]string{
				"KONG_PROXY_LISTEN":  "0.0.0.0:8000 http2",
				"KONG_STREAM_LISTEN": "0.0.0.0:8899",
				"KONG_PORT_MAPS":     "80:8000, 443:8443",
			},
			// KONG_ADMIN_LISTEN deliberately stays an environment variable.
			expectedRemaining: []string{"KONG_DATABASE", "KONG_ADMIN_LISTEN"},
		},
		{
			name: "a managed key sourced from elsewhere is not moved",
			env: []corev1.EnvVar{
				{
					Name: "KONG_PROXY_LISTEN",
					ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "cm"},
							Key:                  "listen",
						},
					},
				},
				{Name: "KONG_PORT_MAPS", Value: "80:8000"},
			},
			expectedManaged: map[string]string{
				"KONG_PORT_MAPS": "80:8000",
			},
			expectedRemaining: []string{"KONG_PROXY_LISTEN"},
		},
		{
			name:              "no env at all",
			env:               nil,
			expectedManaged:   map[string]string{},
			expectedRemaining: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			managed, remaining := ExtractConfWatchEnvVars(tc.env)

			assert.Equal(t, tc.expectedManaged, managed)

			remainingNames := make([]string, 0, len(remaining))
			for _, e := range remaining {
				remainingNames = append(remainingNames, e.Name)
			}
			assert.ElementsMatch(t, tc.expectedRemaining, remainingNames)
		})
	}
}

func TestRenderKongConf(t *testing.T) {
	t.Run("renders the managed keys", func(t *testing.T) {
		out := RenderKongConf(map[string]string{
			"KONG_PROXY_LISTEN":  "0.0.0.0:8000 http2, 0.0.0.0:8443 http2 ssl",
			"KONG_STREAM_LISTEN": "0.0.0.0:8899 reuseport",
			"KONG_PORT_MAPS":     "80:8000, 443:8443",
		})

		assert.Contains(t, out, "proxy_listen = 0.0.0.0:8000 http2, 0.0.0.0:8443 http2 ssl\n")
		assert.Contains(t, out, "stream_listen = 0.0.0.0:8899 reuseport\n")
		assert.Contains(t, out, "port_maps = 80:8000, 443:8443\n")
	})

	t.Run("an unset stream_listen is reset rather than omitted", func(t *testing.T) {
		// `kong reload -c` merges the file on top of the running configuration, so
		// a key that simply disappears keeps its previous value. Removing the last
		// stream listener has to write "off", not nothing.
		out := RenderKongConf(map[string]string{
			"KONG_PROXY_LISTEN": "0.0.0.0:8000",
		})

		assert.Contains(t, out, "stream_listen = off\n")
	})

	t.Run("an empty value is treated as unset", func(t *testing.T) {
		out := RenderKongConf(map[string]string{
			"KONG_PROXY_LISTEN":  "0.0.0.0:8000",
			"KONG_STREAM_LISTEN": "   ",
		})

		assert.Contains(t, out, "stream_listen = off\n")
	})

	t.Run("a key without a reset value is omitted when unset", func(t *testing.T) {
		out := RenderKongConf(map[string]string{
			"KONG_PROXY_LISTEN": "0.0.0.0:8000",
		})

		assert.NotContains(t, out, "port_maps")
	})

	t.Run("output is deterministic", func(t *testing.T) {
		// The in-container watcher compares contents and skips the reload when they
		// are unchanged. Unstable output would reload Kong on every reconciliation.
		env := map[string]string{
			"KONG_PROXY_LISTEN":  "0.0.0.0:8000",
			"KONG_STREAM_LISTEN": "0.0.0.0:8899",
			"KONG_PORT_MAPS":     "80:8000",
		}

		first := RenderKongConf(env)
		for range 20 {
			assert.Equal(t, first, RenderKongConf(env))
		}

		// Keys are sorted.
		var keys []string
		for _, line := range strings.Split(first, "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, _, found := strings.Cut(line, " = ")
			require.True(t, found, "unexpected line %q", line)
			keys = append(keys, key)
		}
		assert.Equal(t, []string{"port_maps", "proxy_listen", "stream_listen"}, keys)
	})
}
