package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newSecret returns the Secret "default/s" holding the given key/value pairs.
func newSecret(data map[string]string) *corev1.Secret {
	secret := &corev1.Secret{
		Name: "s", Namespace: "default",
		Data: map[string][]byte{},
	}
	for k, v := range data {
		secret.Data[k] = []byte(v)
	}
	return secret
}

// newConfigClient returns a fake client preloaded with the given objects.
func newConfigClient(objects ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
}

func TestResolveConfig(t *testing.T) {
	testCases := []struct {
		name        string
		plugin      *configurationv1.KongPlugin
		objects     []client.Object
		expected    string
		expectedNil bool
		expectedErr string
	}{
		{
			name:        "nil plugin is rejected",
			plugin:      nil,
			expectedErr: "plugin cannot be nil",
		},
		{
			name: "plain config is returned untouched",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
				Config: apiextensionsv1.JSON{Raw: []byte(`{"minute":10}`)},
			},
			expected: `{"minute":10}`,
		},
		{
			name: "empty config stays nil when there is nothing to resolve",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
			},
			expectedNil: true,
		},
		{
			name: "configFrom is resolved from the secret",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
				},
			},
			objects:  []client.Object{newSecret(map[string]string{"config": "issuer: https://idp.example.com\nssl_verify: true\n"})},
			expected: `{"issuer":"https://idp.example.com","ssl_verify":true}`,
		},
		{
			name: "configFrom error is wrapped with the plugin identity",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
				ConfigFrom: &configurationv1.ConfigSource{
					SecretValue: configurationv1.SecretValueFromSource{Secret: "missing", Key: "config"},
				},
			},
			expectedErr: "error parsing configFrom for KongPlugin default/p",
		},
		{
			name: "configPatches are applied on top of config",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
				Config: apiextensionsv1.JSON{Raw: []byte(`{"client_id":["cid"],"client_secret":[]}`)},
				ConfigPatches: []configurationv1.ConfigPatch{{
					Path: "/client_secret/0",
					ValueFrom: configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{Secret: "s", Key: "client_secret"},
					},
				}},
			},
			objects:  []client.Object{newSecret(map[string]string{"client_secret": `"shhh"`})},
			expected: `{"client_id":["cid"],"client_secret":["shhh"]}`,
		},
		{
			name: "configPatches error is wrapped with the plugin identity",
			plugin: &configurationv1.KongPlugin{
				Name: "p", Namespace: "default",
				Config: apiextensionsv1.JSON{Raw: []byte(`{}`)},
				ConfigPatches: []configurationv1.ConfigPatch{{
					Path: "/client_secret",
					ValueFrom: configurationv1.ConfigSource{
						SecretValue: configurationv1.SecretValueFromSource{Secret: "missing", Key: "client_secret"},
					},
				}},
			},
			expectedErr: "error applying configPatches for KongPlugin default/p",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveConfig(context.Background(), newConfigClient(tc.objects...), tc.plugin)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			if tc.expectedNil {
				assert.Nil(t, got)
				return
			}
			assert.JSONEq(t, tc.expected, string(got))
		})
	}
}

func TestConfigFromSecret(t *testing.T) {
	testCases := []struct {
		name        string
		ref         configurationv1.SecretValueFromSource
		objects     []client.Object
		expected    string
		expectedErr string
	}{
		{
			name:     "JSON value",
			ref:      configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:  []client.Object{newSecret(map[string]string{"config": `{"minute":10,"policy":"local"}`})},
			expected: `{"minute":10,"policy":"local"}`,
		},
		{
			name:     "YAML value",
			ref:      configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:  []client.Object{newSecret(map[string]string{"config": "minute: 10\npolicy: local\n"})},
			expected: `{"minute":10,"policy":"local"}`,
		},
		{
			name:        "empty value is rejected",
			ref:         configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:     []client.Object{newSecret(map[string]string{"config": ""})},
			expectedErr: "does not hold a JSON or YAML object",
		},
		{
			name:        "literal null is rejected",
			ref:         configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:     []client.Object{newSecret(map[string]string{"config": "null"})},
			expectedErr: "does not hold a JSON or YAML object",
		},
		{
			name:        "value that is neither JSON nor YAML",
			ref:         configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:     []client.Object{newSecret(map[string]string{"config": "\tnot: [valid"})},
			expectedErr: "does not hold a JSON or YAML object",
		},
		{
			name:        "scalar value is not a config object",
			ref:         configurationv1.SecretValueFromSource{Secret: "s", Key: "config"},
			objects:     []client.Object{newSecret(map[string]string{"config": "42"})},
			expectedErr: "does not hold a JSON or YAML object",
		},
		{
			name:        "missing key",
			ref:         configurationv1.SecretValueFromSource{Secret: "s", Key: "absent"},
			objects:     []client.Object{newSecret(map[string]string{"config": "{}"})},
			expectedErr: "no key absent in secret default/s",
		},
		{
			name:        "missing secret",
			ref:         configurationv1.SecretValueFromSource{Secret: "missing", Key: "config"},
			expectedErr: "plugin configuration secret default/missing not found: if it exists, it is not matched by --secret-label-selector",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := configFromSecret(context.Background(), newConfigClient(tc.objects...), "default", tc.ref)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(got))
		})
	}
}

func TestApplyConfigPatches(t *testing.T) {
	patch := func(path, secret, key string) configurationv1.ConfigPatch {
		return configurationv1.ConfigPatch{
			Path: path,
			ValueFrom: configurationv1.ConfigSource{
				SecretValue: configurationv1.SecretValueFromSource{Secret: secret, Key: key},
			},
		}
	}

	testCases := []struct {
		name        string
		rawConfig   json.RawMessage
		patches     []configurationv1.ConfigPatch
		objects     []client.Object
		expected    string
		expectedErr string
	}{
		{
			name:      "no patches leaves the config untouched",
			rawConfig: json.RawMessage(`{"minute":10}`),
			expected:  `{"minute":10}`,
		},
		{
			name:      "single patch into an existing array element",
			rawConfig: json.RawMessage(`{"client_secret":[]}`),
			patches:   []configurationv1.ConfigPatch{patch("/client_secret/0", "s", "client_secret")},
			objects:   []client.Object{newSecret(map[string]string{"client_secret": `"shhh"`})},
			expected:  `{"client_secret":["shhh"]}`,
		},
		{
			name:      "nil config is patched as an empty object",
			rawConfig: nil,
			patches:   []configurationv1.ConfigPatch{patch("/client_secret", "s", "client_secret")},
			objects:   []client.Object{newSecret(map[string]string{"client_secret": `"shhh"`})},
			expected:  `{"client_secret":"shhh"}`,
		},
		{
			name:      "multiple patches are applied in order",
			rawConfig: json.RawMessage(`{"keep":true}`),
			patches: []configurationv1.ConfigPatch{
				patch("/first", "s", "first"),
				patch("/second", "s", "second"),
			},
			objects:  []client.Object{newSecret(map[string]string{"first": `"1"`, "second": `"2"`})},
			expected: `{"keep":true,"first":"1","second":"2"}`,
		},
		{
			name:      "a failing patch aborts the whole resolution",
			rawConfig: json.RawMessage(`{}`),
			patches: []configurationv1.ConfigPatch{
				patch("/ok", "s", "ok"),
				patch("/broken", "s", "absent"),
			},
			objects:     []client.Object{newSecret(map[string]string{"ok": `"1"`})},
			expectedErr: "no key absent in secret default/s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyConfigPatches(context.Background(), newConfigClient(tc.objects...), "default", tc.rawConfig, tc.patches)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(got))
		})
	}
}

func TestApplyJSONPatchFromSecretRef(t *testing.T) {
	testCases := []struct {
		name        string
		raw         json.RawMessage
		path        string
		secretName  string
		key         string
		objects     []client.Object
		expected    string
		expectedErr string
	}{
		{
			name:       "add a top level key",
			raw:        json.RawMessage(`{"a":1}`),
			path:       "/b",
			secretName: "s",
			key:        "b",
			objects:    []client.Object{newSecret(map[string]string{"b": `"two"`})},
			expected:   `{"a":1,"b":"two"}`,
		},
		{
			name:       "replace the document root",
			raw:        json.RawMessage(`{"a":1}`),
			path:       "",
			secretName: "s",
			key:        "root",
			objects:    []client.Object{newSecret(map[string]string{"root": `{"b":2}`})},
			expected:   `{"b":2}`,
		},
		{
			name:       "add to a path that does not exist yet",
			raw:        json.RawMessage(`{}`),
			path:       "/add/headers",
			secretName: "s",
			key:        "headers",
			objects:    []client.Object{newSecret(map[string]string{"headers": `["h1:v1"]`})},
			expected:   `{"add":{"headers":["h1:v1"]}}`,
		},
		{
			name:        "secret value that is not valid JSON",
			raw:         json.RawMessage(`{}`),
			path:        "/b",
			secretName:  "s",
			key:         "b",
			objects:     []client.Object{newSecret(map[string]string{"b": "not-json"})},
			expectedErr: "failed to decode patch for path /b from secret default/s",
		},
		{
			name:        "patch that cannot be applied to the document",
			raw:         json.RawMessage(`{"list":[]}`),
			path:        "/list/5",
			secretName:  "s",
			key:         "v",
			objects:     []client.Object{newSecret(map[string]string{"v": `"x"`})},
			expectedErr: "failed to apply patch for path /list/5 from secret default/s",
		},
		{
			name:        "missing secret",
			raw:         json.RawMessage(`{}`),
			path:        "/b",
			secretName:  "missing",
			key:         "b",
			expectedErr: "plugin configuration secret default/missing not found: if it exists, it is not matched by --secret-label-selector",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyJSONPatchFromSecretRef(
				context.Background(), newConfigClient(tc.objects...), tc.raw, tc.path, "default", tc.secretName, tc.key,
			)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.expected, string(got))
		})
	}
}

func TestSecretValue(t *testing.T) {
	testCases := []struct {
		name        string
		namespace   string
		secretName  string
		key         string
		objects     []client.Object
		expected    string
		expectedErr string
	}{
		{
			name:       "existing key",
			namespace:  "default",
			secretName: "s",
			key:        "config",
			objects:    []client.Object{newSecret(map[string]string{"config": "value"})},
			expected:   "value",
		},
		{
			name:       "existing key holding an empty value",
			namespace:  "default",
			secretName: "s",
			key:        "config",
			objects:    []client.Object{newSecret(map[string]string{"config": ""})},
			expected:   "",
		},
		{
			name:        "missing key",
			namespace:   "default",
			secretName:  "s",
			key:         "absent",
			objects:     []client.Object{newSecret(map[string]string{"config": "value"})},
			expectedErr: "no key absent in secret default/s",
		},
		{
			name:        "missing secret",
			namespace:   "default",
			secretName:  "missing",
			key:         "config",
			expectedErr: "plugin configuration secret default/missing not found: if it exists, it is not matched by --secret-label-selector",
		},
		{
			name:        "secret in another namespace is not visible",
			namespace:   "other",
			secretName:  "s",
			key:         "config",
			objects:     []client.Object{newSecret(map[string]string{"config": "value"})},
			expectedErr: "plugin configuration secret other/s not found: if it exists, it is not matched by --secret-label-selector",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := secretValue(context.Background(), newConfigClient(tc.objects...), tc.namespace, tc.secretName, tc.key)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, string(got))
		})
	}
}
