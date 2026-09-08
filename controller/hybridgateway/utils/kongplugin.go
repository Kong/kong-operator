package utils

import (
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
)

// KongPluginSecretNames returns the names of the Secrets that the given KongPlugin sources its
// configuration from, through spec.configFrom and spec.configPatches. The Secrets always live in
// the KongPlugin's own namespace, so only their names are returned. Names are deduplicated and
// kept in the order they are first encountered; an empty name is skipped.
func KongPluginSecretNames(plugin *configurationv1.KongPlugin) []string {
	if plugin == nil {
		return nil
	}

	var names []string
	seen := make(map[string]struct{})
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if plugin.ConfigFrom != nil {
		add(plugin.ConfigFrom.SecretValue.Secret)
	}
	for _, patch := range plugin.ConfigPatches {
		add(patch.ValueFrom.SecretValue.Secret)
	}

	return names
}
