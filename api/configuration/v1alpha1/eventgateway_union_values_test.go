package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

// TestEventGatewayUnionValues converts specs whose values name a sibling key:
// they are data, not union discriminators.
func TestEventGatewayUnionValues(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	// request returns the JSON of the create request of the object in manifest.
	request := func(t *testing.T, manifest string, obj any, convert func() (any, error)) map[string]any {
		t.Helper()
		require.NoError(t, yaml.Unmarshal([]byte(manifest), obj))
		req, err := convert()
		require.NoError(t, err)
		data, err := json.Marshal(req)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		return m
	}

	t.Run("saslPlain username password", func(t *testing.T) {
		obj := &EventGatewayBackendCluster{}
		m := request(t, `
spec:
  apiSpec:
    name: backend
    bootstrapServers: ["kafka:9092"]
    authentication: {type: saslPlain, saslPlain: {username: password, password: secret}}
    tls: {enabled: Disabled}
`, obj, func() (any, error) { return obj.ToCreateBackendClusterRequest(t.Context(), cl) })
		auth := m["authentication"].(map[string]any)
		assert.Equal(t, "sasl_plain", auth["type"])
		assert.Equal(t, "password", auth["username"])
		assert.Equal(t, "secret", auth["password"])
	})

	virtualCluster := func(t *testing.T, apiSpec string) map[string]any {
		t.Helper()
		obj := &EventGatewayVirtualCluster{}
		return request(t, `
spec:
  apiSpec:
    name: vcluster
    dnsLabel: vcluster
    aclMode: passthrough
    authentication: [{type: anonymous}]
`+apiSpec, obj, func() (any, error) {
			// As the reconciler sets it before converting.
			obj.SetEventGatewayBackendClusterID("backend-id")
			return obj.ToCreateVirtualClusterRequest()
		})
	}

	t.Run("topic alias topic", func(t *testing.T) {
		m := virtualCluster(t, `
    namespace: {mode: hide_prefix, prefix: p_}
    topicAliases: [{alias: topic, topic: orders}]
`)
		aliases := m["topic_aliases"].([]any)
		require.Len(t, aliases, 1)
		alias := aliases[0].(map[string]any)
		assert.Equal(t, "topic", alias["alias"])
		assert.Equal(t, "orders", alias["topic"])
	})

	t.Run("namespace prefix mode", func(t *testing.T) {
		m := virtualCluster(t, `
    namespace: {mode: hide_prefix, prefix: mode}
`)
		assert.Equal(t, map[string]any{"mode": "hide_prefix", "prefix": "mode"}, m["namespace"])
	})

	for name, set := range map[string]map[string]any{
		"header key value": {"key": "value", "value": "x"},
		"header value key": {"key": "x-src", "value": "key"},
	} {
		t.Run(name, func(t *testing.T) {
			obj := &EventGatewayVirtualClusterProducePolicy{}
			setYAML, err := json.Marshal(set)
			require.NoError(t, err)
			m := request(t, `
spec:
  apiSpec:
    type: modifyHeaders
    modifyHeaders:
      name: headers
      config:
        actions: [{op: set, set: `+string(setYAML)+`}]
`, obj, func() (any, error) {
				return obj.ToCreateEventGatewayVirtualClusterProducePolicyRequest(t.Context(), cl)
			})
			body := m["EventGatewayProducePolicyCreate"]
			require.NotNil(t, body, "request: %v", m)
			config := body.(map[string]any)["config"].(map[string]any)
			assert.Equal(t, []any{map[string]any{"op": "set", "key": set["key"], "value": set["value"]}}, config["actions"])
		})
	}
}
