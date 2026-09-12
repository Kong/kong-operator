package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
)

func TestKongPluginSecretNames(t *testing.T) {
	configPatch := func(secret string) configurationv1.ConfigPatch {
		return configurationv1.ConfigPatch{
			Path: "/client_secret",
			ValueFrom: configurationv1.ConfigSource{
				SecretValue: configurationv1.SecretValueFromSource{Secret: secret, Key: "client_secret"},
			},
		}
	}

	testCases := []struct {
		name     string
		plugin   *configurationv1.KongPlugin
		expected []string
	}{
		{
			name:     "nil plugin",
			plugin:   nil,
			expected: nil,
		},
		{
			name:     "plugin without secret references",
			plugin:   &configurationv1.KongPlugin{},
			expected: nil,
		},
		{
			name: "configFrom reference",
			plugin: &configurationv1.KongPlugin{
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "cfg", Key: "config"},
				},
			},
			expected: []string{"cfg"},
		},
		{
			name: "configPatches references",
			plugin: &configurationv1.KongPlugin{
				ConfigPatches: []configurationv1.ConfigPatch{configPatch("first"), configPatch("second")},
			},
			expected: []string{"first", "second"},
		},
		{
			name: "duplicate references are deduplicated",
			plugin: &configurationv1.KongPlugin{
				ConfigPatches: []configurationv1.ConfigPatch{configPatch("same"), configPatch("same")},
			},
			expected: []string{"same"},
		},
		{
			name: "configFrom and configPatches are merged",
			plugin: &configurationv1.KongPlugin{
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "cfg", Key: "config"},
				},
				ConfigPatches: []configurationv1.ConfigPatch{configPatch("cfg"), configPatch("patch")},
			},
			expected: []string{"cfg", "patch"},
		},
		{
			name: "empty secret names are skipped",
			plugin: &configurationv1.KongPlugin{
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Key: "config"},
				},
				ConfigPatches: []configurationv1.ConfigPatch{configPatch(""), configPatch("patch")},
			},
			expected: []string{"patch"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, KongPluginSecretNames(tc.plugin))
		})
	}
}
