package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// eventGatewayNameBases are valid manifests, by name, on top of
// eventGatewayArrayBases.
var eventGatewayNameBases = map[string]string{
	"staticKey": `
kind: EventGatewayStaticKey
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: key
    value: {type: inline, value: Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy1vayE=}
`,
	"trustBundle": `
kind: EventGatewayTLSTrustBundle
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: bundle
    config:
      trustedCa: {type: inline, value: ca}
`,
	"schemaRegistry": `
kind: EventGatewaySchemaRegistry
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    type: confluent
    confluent:
      name: registry
      config:
        endpoint: https://registry.example.com
        schemaType: avro
        authentication: {type: basic, basic: {username: u, password: {type: inline, value: p}}}
`,
	"dataPlaneCertificate": `
kind: EventGatewayDataPlaneCertificate
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: certificate
    certificate: {type: secretRef, secretRef: {name: tls, key: tls.crt}}
`,
	"backendClusterTLS": `
kind: EventGatewayBackendCluster
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gw}
  apiSpec:
    name: backend
    bootstrapServers: ["kafka:9092"]
    authentication: {type: anonymous, anonymous: {}}
    tls:
      enabled: Enabled
      clientIdentity:
        certificate: {type: inline, value: cert}
        key: {type: inline, value: key}
`,
	"tlsListenerPolicyInline": `
kind: EventGatewayListenerPolicy
spec:
  eventGatewayListenerRef: {type: namespacedRef, namespacedRef: {name: listener}}
  apiSpec:
    type: tlsServer
    tlsServer:
      name: tls
      config:
        certificates:
          - certificate: {type: inline, value: cert}
            key: {type: inline, value: key}
`,
}

// TestEventGatewayNamesAndSensitiveValues sends raw JSON, as kubectl does.
func TestEventGatewayNamesAndSensitiveValues(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)

	const namePatternErr = "in body should match '^[\\p{L}\\p{N}][\\p{L}\\p{N} _\\-\\.:/+']*[\\p{L}\\p{N}]$'"
	const emptyErr = "an inline value must not be empty"

	type tcase struct {
		name    string
		base    string
		path    []string
		value   any
		wantErr string
	}
	var tests []tcase

	names := []struct {
		base string
		path []string
	}{
		{"backendCluster", []string{"apiSpec", "name"}},
		{"virtualCluster", []string{"apiSpec", "name"}},
		{"listener", []string{"apiSpec", "name"}},
		{"tlsListenerPolicy", []string{"apiSpec", "tlsServer", "name"}},
		{"aclPolicy", []string{"apiSpec", "acls", "name"}},
		{"encryptPolicy", []string{"apiSpec", "encrypt", "name"}},
		{"decryptPolicy", []string{"apiSpec", "decrypt", "name"}},
		{"staticKey", []string{"apiSpec", "name"}},
		{"trustBundle", []string{"apiSpec", "name"}},
		{"schemaRegistry", []string{"apiSpec", "confluent", "name"}},
		{"dataPlaneCertificate", []string{"apiSpec", "name"}},
	}
	for _, n := range names {
		tests = append(tests,
			tcase{name: n.base + ": valid", base: n.base},
			tcase{name: n.base + ": name with !", base: n.base, path: n.path, value: "bc!", wantErr: namePatternErr},
			tcase{name: n.base + ": one-character name", base: n.base, path: n.path, value: "b", wantErr: namePatternErr},
			tcase{name: n.base + ": name starting with -", base: n.base, path: n.path, value: "-x", wantErr: namePatternErr},
			tcase{name: n.base + ": non-ASCII name", base: n.base, path: n.path, value: "Zürich café 東京"},
			tcase{name: n.base + ": every allowed separator", base: n.base, path: n.path, value: "a b_c-d.e:f/g+h'i"},
		)
	}

	// The sensitive values that must not be empty.
	values := []struct {
		base string
		path []string
	}{
		{"staticKey", []string{"apiSpec", "value"}},
		{"trustBundle", []string{"apiSpec", "config", "trustedCa"}},
		{"schemaRegistry", []string{"apiSpec", "confluent", "config", "authentication", "basic", "password"}},
		{"backendClusterTLS", []string{"apiSpec", "tls", "clientIdentity", "certificate"}},
		{"backendClusterTLS", []string{"apiSpec", "tls", "clientIdentity", "key"}},
		{"dataPlaneCertificate", []string{"apiSpec", "certificate"}},
	}
	for _, v := range values {
		label := v.base + " " + v.path[len(v.path)-1]
		tests = append(tests,
			tcase{name: label + ": empty inline value", base: v.base, path: v.path, value: map[string]any{"type": "inline", "value": ""}, wantErr: emptyErr},
			tcase{name: label + ": inline value", base: v.base, path: v.path, value: map[string]any{"type": "inline", "value": "x"}},
			tcase{name: label + ": from a Secret", base: v.base, path: v.path, value: map[string]any{"type": "secretRef", "secretRef": map[string]any{"name": "s", "key": "k"}}},
		)
	}
	certificates := []string{"apiSpec", "tlsServer", "config", "certificates"}
	tests = append(tests,
		tcase{name: "listener TLS: valid inline", base: "tlsListenerPolicyInline"},
		tcase{name: "listener TLS: empty certificate", base: "tlsListenerPolicyInline", path: certificates, value: []any{map[string]any{
			"certificate": map[string]any{"type": "inline", "value": ""}, "key": map[string]any{"type": "inline", "value": "key"},
		}}, wantErr: emptyErr},
		tcase{name: "listener TLS: empty key", base: "tlsListenerPolicyInline", path: certificates, value: []any{map[string]any{
			"certificate": map[string]any{"type": "inline", "value": "cert"}, "key": map[string]any{"type": "inline", "value": ""},
		}}, wantErr: emptyErr},
	)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, ok := eventGatewayNameBases[tc.base]
			if !ok {
				manifest = eventGatewayArrayBases[tc.base]
			}
			require.NotEmpty(t, manifest, tc.base)
			obj := &unstructured.Unstructured{}
			require.NoError(t, yaml.Unmarshal([]byte(manifest), &obj.Object))
			obj.SetAPIVersion("configuration.konghq.com/v1alpha1")
			obj.SetNamespace(ns.Name)
			obj.SetGenerateName("name-")
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
