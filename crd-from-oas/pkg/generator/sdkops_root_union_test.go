package generator

import (
	"go/format"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestGenerateSDKOps_RootUnionUsesDiscriminatorJSONNames(t *testing.T) {
	t.Parallel()

	g := NewGenerator(Config{
		APIVersion: "v1alpha1",
	})

	schema := &parser.Schema{
		OneOf: []*parser.Property{
			{
				RefName: "EventGatewayTLSListenerPolicy",
				Properties: []*parser.Property{
					{Name: "enabled", Type: "boolean"},
					{
						Name: "config",
						Type: "object",
						Properties: []*parser.Property{
							{Name: "allow_plaintext", Type: "boolean"},
						},
					},
				},
			},
			{
				RefName: "ForwardToVirtualClusterPolicy",
				Properties: []*parser.Property{
					{Name: "enabled", Type: "boolean"},
				},
			},
		},
		DiscriminatorMapping: map[string]string{
			"tls_server":                 "EventGatewayTLSListenerPolicy",
			"forward_to_virtual_cluster": "ForwardToVirtualClusterPolicy",
		},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/operations.CreateEventGatewayListenerPolicyRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/operations.UpdateEventGatewayListenerPolicyRequest"},
		},
	}

	content, err := g.generateSDKOps("EventGatewayListenerPolicy", schema, opsConfig)
	require.NoError(t, err)

	_, err = format.Source([]byte(content))
	require.NoError(t, err)

	assert.Contains(t, content, `selected = payload["tls_server"]`)
	assert.Contains(t, content, `selected = payload["forward_to_virtual_cluster"]`)
	assert.Contains(t, content, `"tls_server",`)
	assert.Contains(t, content, `"forward_to_virtual_cluster",`)
	assert.Contains(t, content, `withType["type"] = typeValue`)
	assert.Contains(t, content, `var body sdkkonnectcomp.EventGatewayListenerPolicyUpdate`)
	assert.Contains(t, content, `failed to unmarshal into EventGatewayListenerPolicyUpdate`)
	assert.Contains(t, content, `failed to unmarshal into EventGatewayTLSListenerPolicy`)
	assert.NotContains(t, content, `payload["eventgatewaytlslisten"]`)
	assert.NotContains(t, content, `payload["forwardtovirtualclust"]`)
}
