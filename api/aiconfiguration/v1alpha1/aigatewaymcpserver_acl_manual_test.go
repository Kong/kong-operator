package v1alpha1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

// TestAIGatewayMCPServer_OAuthAccessACLsWireShape guards against a regression
// where an oauth access-configured listener request always carried an empty
// "acls"/"default_tool_acls" object. Konnect rejects that object because its
// properties (allow, deny, allow_when, deny_when) are required, so it must be
// left out unless the CR lists consumer group references. An explicitly empty
// list (allow: []) decodes to a non-nil empty slice and must be left out too.
func TestAIGatewayMCPServer_OAuthAccessACLsWireShape(t *testing.T) {
	t.Parallel()

	const spec = `metadata:
  name: mcp-passthrough
  namespace: default
spec:
  apiSpec:
    type: passthrough-listener
    passthrough-listener:
      name: mcp-passthrough
      displayName: mcp-passthrough
      enabled: Enabled
      access:
        aclAttributeType: oauthAccessToken
        oauthAccessToken:
          accessTokenClaimField: .user.email%s
      config:
        url: https://flights-mcp.kongair.example
`

	access := func(t *testing.T, aclsBlock string) map[string]any {
		t.Helper()

		obj := &AIGatewayMCPServer{}
		require.NoError(t, yaml.Unmarshal(fmt.Appendf(nil, spec, aclsBlock), obj))

		scheme := runtime.NewScheme()
		require.NoError(t, AddToScheme(scheme))
		group := &AIGatewayConsumerGroup{ObjectMeta: metav1.ObjectMeta{Name: "internal-users", Namespace: "default"}}
		group.SetKonnectID("cg-kid")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(group).Build()

		req, err := obj.ToCreateAIGatewayMCPServerRequest(t.Context(), cl)
		require.NoError(t, err)

		raw, err := json.Marshal(req)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(raw, &payload))
		access, ok := payload["access"].(map[string]any)
		require.True(t, ok, "request payload carries no access block: %s", raw)
		return access
	}

	t.Run("no acls configured", func(t *testing.T) {
		t.Parallel()
		got := access(t, "")
		require.NotContains(t, got, "acls")
		require.NotContains(t, got, "default_tool_acls")
	})

	t.Run("empty acls lists configured", func(t *testing.T) {
		t.Parallel()
		got := access(t, `
          acls:
            allow: []
          defaultToolAcls:
            allow: []`)
		require.NotContains(t, got, "acls")
		require.NotContains(t, got, "default_tool_acls")
	})

	t.Run("acls configured", func(t *testing.T) {
		t.Parallel()
		got := access(t, `
          acls:
            allow:
              - name: internal-users`)
		require.Contains(t, got, "acls")
	})
}
