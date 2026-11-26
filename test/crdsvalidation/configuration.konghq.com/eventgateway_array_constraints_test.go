package configuration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// eventGatewayArrayBases are valid manifests, by name.
var eventGatewayArrayBases = map[string]string{
	"backendCluster": `
kind: EventGatewayBackendCluster
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: backend
    bootstrapServers: ["kafka:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls: {enabled: Enabled, tlsVersions: [tls13]}
`,
	"virtualCluster": `
kind: EventGatewayVirtualCluster
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: backend}}
  apiSpec:
    name: vcluster
    dnsLabel: vcluster
    aclMode: passthrough
    authentication: [{type: anonymous}]
    namespace: {mode: hide_prefix, prefix: p_, additional: {topics: [{type: exactList, exactList: {exactList: [{backend: orders}]}}]}}
`,
	"listener": `
kind: EventGatewayListener
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: listener
    addresses: ["0.0.0.0"]
    ports: [9092, "19092-19095"]
`,
	"tlsListenerPolicy": `
kind: EventGatewayListenerPolicy
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: tls
      config:
        certificates:
          - certificate: {type: secretRef, secretRef: {name: tls, key: tls.crt}}
            key: {type: secretRef, secretRef: {name: tls, key: tls.key}}
        clientAuthentication: {mode: required, tlsTrustBundles: [{name: bundle}]}
`,
	"aclPolicy": `
kind: EventGatewayVirtualClusterPolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: acls
    acls:
      name: acls
      config:
        rules:
          - action: allow
            resourceType: topic
            operations: [{name: all}]
            resourceNames: {type: stat, stat: [{match: "*"}]}
`,
	"requestRuleValidatorPolicy": `
kind: EventGatewayVirtualClusterPolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: requestRuleValidator
    requestRuleValidator:
      name: rules
      config:
        requests:
          - type: createTopics
            createTopics: {rules: [{action: reject, description: d, rule: "{{ true }}"}]}
`,
	"encryptPolicy": `
kind: EventGatewayVirtualClusterProducePolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: encrypt
    encrypt:
      name: encrypt
      config:
        failureMode: error
        partOfRecord: [value]
        encryptionKey: {type: static, static: {key: {name: key}}}
`,
	"encryptFieldsPolicy": `
kind: EventGatewayVirtualClusterProducePolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: encryptFields
    parentPolicyRef: {name: validate}
    encryptFields:
      name: encrypt-fields
      config:
        failureMode: reject
        encryptFields:
          - paths: {type: array, array: [{match: ssn}]}
            encryptionKey: {type: static, static: {key: {name: key}}}
`,
	"decryptPolicy": `
kind: EventGatewayVirtualClusterConsumePolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: decrypt
    decrypt:
      name: decrypt
      config:
        failureMode: error
        partOfRecord: [value]
        keySources: [{type: static, static: {}}]
`,
	"maskFieldsPolicy": `
kind: EventGatewayVirtualClusterConsumePolicy
spec:
  eventGatewayVirtualClusterRef: {type: namespacedRef, namespacedRef: {name: vcluster}}
  apiSpec:
    type: maskFields
    parentPolicyRef: {name: validate}
    maskFields:
      name: mask
      config:
        failureMode: error
        maskFields:
          - paths: {type: array, array: [{match: ssn}]}
            strategy: {type: replace, replace: {phrase: "***"}}
`,
}

// TestEventGatewayArrayConstraints sends raw JSON: a typed client drops empty
// lists, which hides minItems.
func TestEventGatewayArrayConstraints(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)

	many := func(n int, item func(i int) any) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = item(i)
		}
		return out
	}
	match := func(i int) any {
		return map[string]any{"match": "f" + strings.Repeat("x", i%5) + string(rune('a'+i%26))}
	}

	tests := []struct {
		name  string
		base  string
		path  []string
		value any
		// wantErr is "" when the manifest is admitted.
		wantErr string
	}{
		{name: "backend cluster: valid", base: "backendCluster"},
		{name: "bootstrapServers empty", base: "backendCluster", path: []string{"apiSpec", "bootstrapServers"}, value: []any{}, wantErr: "spec.apiSpec.bootstrapServers: Invalid value: 0: spec.apiSpec.bootstrapServers in body should have at least 1 items"},
		{name: "bootstrapServers item without a port", base: "backendCluster", path: []string{"apiSpec", "bootstrapServers"}, value: []any{"kafka"}, wantErr: "spec.apiSpec.bootstrapServers[0]: Invalid value: \"kafka\": spec.apiSpec.bootstrapServers[0] in body should match"},
		{name: "bootstrapServers item empty", base: "backendCluster", path: []string{"apiSpec", "bootstrapServers"}, value: []any{""}, wantErr: "spec.apiSpec.bootstrapServers[0] in body should be at least 1 chars long"},
		{name: "bootstrapServers IPv6 item", base: "backendCluster", path: []string{"apiSpec", "bootstrapServers"}, value: []any{"[::1]:9092"}},
		{name: "tlsVersions tls11", base: "backendCluster", path: []string{"apiSpec", "tls", "tlsVersions"}, value: []any{"tls11"}, wantErr: "spec.apiSpec.tls.tlsVersions[0]: Unsupported value: \"tls11\""},

		{name: "virtual cluster: valid", base: "virtualCluster"},
		{name: "authentication empty", base: "virtualCluster", path: []string{"apiSpec", "authentication"}, value: []any{}, wantErr: "spec.apiSpec.authentication in body should have at least 1 items"},
		{name: "additional topics exactList empty", base: "virtualCluster", path: []string{"apiSpec", "namespace", "additional", "topics"}, value: []any{map[string]any{"type": "exactList", "exactList": map[string]any{"exactList": []any{}}}}, wantErr: "should have at least 1 items"},
		{name: "saslPlain principals empty", base: "virtualCluster", path: []string{"apiSpec", "authentication"}, value: []any{map[string]any{"type": "saslPlain", "saslPlain": map[string]any{"mediation": "terminate", "principals": []any{}}}}, wantErr: "principals in body should have at least 1 items"},

		{name: "listener: valid", base: "listener"},
		{name: "addresses empty", base: "listener", path: []string{"apiSpec", "addresses"}, value: []any{}, wantErr: "spec.apiSpec.addresses in body should have at least 1 items"},
		{name: "addresses item empty", base: "listener", path: []string{"apiSpec", "addresses"}, value: []any{""}, wantErr: "spec.apiSpec.addresses[0] in body should be at least 1 chars long"},
		{name: "ports empty", base: "listener", path: []string{"apiSpec", "ports"}, value: []any{}, wantErr: "spec.apiSpec.ports in body should have at least 1 items"},
		{name: "ports 1 and 65535", base: "listener", path: []string{"apiSpec", "ports"}, value: []any{int64(1), int64(65535), "1-65535"}},
		{name: "port 0", base: "listener", path: []string{"apiSpec", "ports"}, value: []any{int64(0)}, wantErr: "an integer port must be between 1 and 65535"},
		{name: "port 70000", base: "listener", path: []string{"apiSpec", "ports"}, value: []any{int64(9092), int64(70000)}, wantErr: "an integer port must be between 1 and 65535"},
		{name: "ports: 64", base: "listener", path: []string{"apiSpec", "ports"}, value: many(64, func(i int) any { return int64(10000 + i) })},
		{name: "ports: 65", base: "listener", path: []string{"apiSpec", "ports"}, value: many(65, func(i int) any { return int64(10000 + i) }), wantErr: "must have at most 64 items"},
		// String ports aren't checked.
		{name: "string port not checked", base: "listener", path: []string{"apiSpec", "ports"}, value: []any{"abc"}},

		{name: "TLS listener policy: valid", base: "tlsListenerPolicy"},
		{name: "TLS certificates empty", base: "tlsListenerPolicy", path: []string{"apiSpec", "tlsServer", "config", "certificates"}, value: []any{}, wantErr: "spec.apiSpec.tlsServer.config.certificates in body should have at least 1 items"},
		{name: "TLS trust bundles empty", base: "tlsListenerPolicy", path: []string{"apiSpec", "tlsServer", "config", "clientAuthentication", "tlsTrustBundles"}, value: []any{}, wantErr: "tlsTrustBundles in body should have at least 1 items"},

		{name: "ACL policy: valid", base: "aclPolicy"},
		{name: "ACL rules empty", base: "aclPolicy", path: []string{"apiSpec", "acls", "config", "rules"}, value: []any{}, wantErr: "spec.apiSpec.acls.config.rules in body should have at least 1 items"},
		{name: "ACL resource names: 51", base: "aclPolicy", path: []string{"apiSpec", "acls", "config", "rules"}, value: []any{map[string]any{
			"action": "allow", "resourceType": "topic", "operations": []any{map[string]any{"name": "all"}},
			"resourceNames": map[string]any{"type": "stat", "stat": many(51, match)},
		}}, wantErr: "must have at most 50 items"},
		{name: "ACL resource names: 50", base: "aclPolicy", path: []string{"apiSpec", "acls", "config", "rules"}, value: []any{map[string]any{
			"action": "allow", "resourceType": "topic", "operations": []any{map[string]any{"name": "all"}},
			"resourceNames": map[string]any{"type": "stat", "stat": many(50, match)},
		}}},

		{name: "request rule validator: valid", base: "requestRuleValidatorPolicy"},
		{name: "request rules empty", base: "requestRuleValidatorPolicy", path: []string{"apiSpec", "requestRuleValidator", "config", "requests"}, value: []any{}, wantErr: "requests in body should have at least 1 items"},

		{name: "encrypt policy: valid", base: "encryptPolicy"},
		{name: "encrypt partOfRecord headers", base: "encryptPolicy", path: []string{"apiSpec", "encrypt", "config", "partOfRecord"}, value: []any{"headers"}, wantErr: "spec.apiSpec.encrypt.config.partOfRecord[0]: Unsupported value: \"headers\""},
		{name: "encrypt partOfRecord empty", base: "encryptPolicy", path: []string{"apiSpec", "encrypt", "config", "partOfRecord"}, value: []any{}, wantErr: "partOfRecord in body should have at least 1 items"},

		{name: "encryptFields policy: valid", base: "encryptFieldsPolicy"},
		{name: "encryptFields empty", base: "encryptFieldsPolicy", path: []string{"apiSpec", "encryptFields", "config", "encryptFields"}, value: []any{}, wantErr: "encryptFields in body should have at least 1 items"},
		{name: "encryptFields paths empty", base: "encryptFieldsPolicy", path: []string{"apiSpec", "encryptFields", "config", "encryptFields"}, value: []any{map[string]any{
			"paths": map[string]any{"type": "array", "array": []any{}}, "encryptionKey": map[string]any{"type": "static", "static": map[string]any{"key": map[string]any{"name": "key"}}},
		}}, wantErr: "array in body should have at least 1 items"},
		{name: "encryptFields paths: 51", base: "encryptFieldsPolicy", path: []string{"apiSpec", "encryptFields", "config", "encryptFields"}, value: []any{map[string]any{
			"paths": map[string]any{"type": "array", "array": many(51, match)}, "encryptionKey": map[string]any{"type": "static", "static": map[string]any{"key": map[string]any{"name": "key"}}},
		}}, wantErr: "must have at most 50 items"},

		{name: "decrypt policy: valid", base: "decryptPolicy"},
		{name: "decrypt partOfRecord headers", base: "decryptPolicy", path: []string{"apiSpec", "decrypt", "config", "partOfRecord"}, value: []any{"headers"}, wantErr: "spec.apiSpec.decrypt.config.partOfRecord[0]: Unsupported value: \"headers\""},
		{name: "decrypt keySources empty", base: "decryptPolicy", path: []string{"apiSpec", "decrypt", "config", "keySources"}, value: []any{}, wantErr: "keySources in body should have at least 1 items"},

		{name: "maskFields policy: valid", base: "maskFieldsPolicy"},
		{name: "maskFields empty", base: "maskFieldsPolicy", path: []string{"apiSpec", "maskFields", "config", "maskFields"}, value: []any{}, wantErr: "maskFields in body should have at least 1 items"},
		{name: "maskFields paths: 51", base: "maskFieldsPolicy", path: []string{"apiSpec", "maskFields", "config", "maskFields"}, value: []any{map[string]any{
			"paths": map[string]any{"type": "array", "array": many(51, match)}, "strategy": map[string]any{"type": "replace", "replace": map[string]any{"phrase": "***"}},
		}}, wantErr: "must have at most 50 items"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal([]byte(eventGatewayArrayBases[tc.base]), &obj.Object))
			obj.SetAPIVersion("configuration.konghq.com/v1alpha1")
			obj.SetNamespace(ns.Name)
			obj.SetGenerateName("array-")
			if tc.path != nil {
				require.NoError(t, unstructured.SetNestedField(obj.Object, tc.value, append([]string{"spec"}, tc.path...)...))
			}
			err := cl.Create(ctx, obj, client.DryRunAll)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
