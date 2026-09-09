package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
)

// rawPatchPattern is the JSON patch (RFC6902) document template used to inject a
// secret-sourced value at a given path of the plugin configuration.
// Ported from ingress-controller/internal/dataplane/kongstate/plugin.go.
const rawPatchPattern = `[{"op":"%s","path":"%s","value":%s}]`

const (
	// jsonPatchOpAdd is the RFC6902 "add" operation.
	jsonPatchOpAdd = "add"
	// jsonPatchOpReplace is the RFC6902 "replace" operation. It is used instead of
	// "add" when patching the document root, which the jsonpatch package cannot "add" to.
	jsonPatchOpReplace = "replace"
)

// ResolveConfig returns the effective configuration of the given KongPlugin, resolving the Secret
// references declared in spec.configFrom and spec.configPatches.
//
// The two fields are mutually exclusive (enforced by the KongPlugin CRD validation rules), so:
//   - when spec.configFrom is set, the whole configuration is read from the referenced Secret key,
//     which may hold either JSON or YAML;
//   - otherwise spec.config is used as-is, with every spec.configPatches entry applied on top of it
//     as an RFC6902 patch whose value is read from the referenced Secret key.
//
// When neither field is set the raw spec.config is returned untouched, including when it is nil.
func ResolveConfig(ctx context.Context, cl client.Client, plugin *configurationv1.KongPlugin) (json.RawMessage, error) {
	if plugin == nil {
		return nil, errors.New("plugin cannot be nil")
	}

	if plugin.ConfigFrom != nil {
		config, err := configFromSecret(ctx, cl, plugin.Namespace, plugin.ConfigFrom.SecretValue)
		if err != nil {
			return nil, fmt.Errorf("error parsing configFrom for KongPlugin %s/%s: %w", plugin.Namespace, plugin.Name, err)
		}
		return config, nil
	}

	if len(plugin.ConfigPatches) == 0 {
		return plugin.Config.Raw, nil
	}

	config, err := applyConfigPatches(ctx, cl, plugin.Namespace, plugin.Config.Raw, plugin.ConfigPatches)
	if err != nil {
		return nil, fmt.Errorf("error applying configPatches for KongPlugin %s/%s: %w", plugin.Namespace, plugin.Name, err)
	}
	return config, nil
}

// configFromSecret reads the plugin configuration from the key of the Secret referenced by the
// given source, in the given namespace. The stored value may be either a JSON or a YAML object, and
// is always returned as JSON. Anything that is not an object is rejected.
func configFromSecret(
	ctx context.Context,
	cl client.Client,
	namespace string,
	ref configurationv1.SecretValueFromSource,
) (json.RawMessage, error) {
	value, err := secretValue(ctx, cl, namespace, ref.Secret, ref.Key)
	if err != nil {
		return nil, err
	}

	var config map[string]any
	if jsonErr := json.Unmarshal(value, &config); jsonErr != nil {
		if yamlErr := yaml.Unmarshal(value, &config); yamlErr != nil {
			return nil, fmt.Errorf("key %s in secret %s/%s does not hold a JSON or YAML object", ref.Key, namespace, ref.Secret)
		}
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config from secret %s/%s: %w", namespace, ref.Secret, err)
	}
	return raw, nil
}

// applyConfigPatches applies every configPatches entry on top of the given raw configuration,
// resolving each patched value from the referenced Secret key in the given namespace. A nil or
// empty raw configuration is patched as if it were an empty JSON object.
func applyConfigPatches(
	ctx context.Context,
	cl client.Client,
	namespace string,
	rawConfig json.RawMessage,
	patches []configurationv1.ConfigPatch,
) (json.RawMessage, error) {
	raw := rawConfig
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}

	for _, patch := range patches {
		var err error
		raw, err = applyJSONPatchFromSecretRef(
			ctx,
			cl,
			raw,
			patch.Path,
			namespace,
			patch.ValueFrom.SecretValue.Secret,
			patch.ValueFrom.SecretValue.Key,
		)
		if err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// applyJSONPatchFromSecretRef applies a single RFC6902 patch to raw, injecting at path the value
// held by the given key of the given Secret. The raw secret bytes are interpolated into the patch
// document as-is, so they must themselves be valid JSON.
// Ported from ingress-controller/internal/dataplane/kongstate/plugin.go.
func applyJSONPatchFromSecretRef(
	ctx context.Context,
	cl client.Client,
	raw json.RawMessage,
	path string,
	namespace string,
	secretName string,
	key string,
) (json.RawMessage, error) {
	value, err := secretValue(ctx, cl, namespace, secretName, key)
	if err != nil {
		return nil, err
	}

	// RFC6902 specifies the behavior of applying "add" on the document root, but the jsonpatch
	// package cannot "add" on the root path (path=""), so "replace" is used to set the whole
	// document instead. See https://github.com/evanphx/json-patch/issues/188.
	op := jsonPatchOpAdd
	if path == "" {
		op = jsonPatchOpReplace
	}

	rawPatch := fmt.Sprintf(rawPatchPattern, op, path, string(value))
	p, err := jsonpatch.DecodePatch([]byte(rawPatch))
	if err != nil {
		return nil, fmt.Errorf("failed to decode patch for path %s from secret %s/%s: %w", path, namespace, secretName, err)
	}

	// EnsurePathExistsOnAdd allows adding a subpath of a path that does not exist yet, e.g. applying
	// {"op":"add","path":"/add/headers","value":[{"h1":"v1"}]} on `{}`.
	opts := jsonpatch.NewApplyOptions()
	opts.EnsurePathExistsOnAdd = true
	patched, err := p.ApplyWithOptions(raw, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply patch for path %s from secret %s/%s: %w", path, namespace, secretName, err)
	}

	return patched, nil
}

// secretValue returns the bytes held by the given key of the given Secret.
func secretValue(ctx context.Context, cl client.Client, namespace, name, key string) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, fmt.Errorf("failed to fetch plugin configuration secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok {
		return nil, fmt.Errorf("no key %s in secret %s/%s", key, namespace, name)
	}
	return value, nil
}
