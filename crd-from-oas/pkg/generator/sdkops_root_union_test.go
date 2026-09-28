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
	assert.NotContains(t, content, `len(selectedMap)+1`)
	assert.Contains(t, content, `var body sdkkonnectcomp.EventGatewayListenerPolicyUpdate`)
	assert.Contains(t, content, `failed to unmarshal into EventGatewayListenerPolicyUpdate`)
	assert.Contains(t, content, `failed to unmarshal into EventGatewayTLSListenerPolicy`)
	assert.NotContains(t, content, `payload["eventgatewaytlslisten"]`)
	assert.NotContains(t, content, `payload["forwardtovirtualclust"]`)
}

func TestGenerateSDKOps_RootUnionFlattenSkipsFreeformFields(t *testing.T) {
	// Root-union entities select the variant payload before flattening, and
	// their free-form paths are stored with the variant's JSON name as the
	// first segment (they are shared with the full-payload-scope rename walk).
	// The flatten call must therefore strip the variant prefix via
	// flattenSDKUnionsExceptUnder instead of flattening unconditionally.
	g := NewGenerator(Config{APIVersion: "v1alpha1"})

	schema := &parser.Schema{
		OneOf: []*parser.Property{
			{
				RefName: "EventGatewayTLSListenerPolicy",
				Properties: []*parser.Property{
					{Name: "enabled", Type: "boolean"},
					{
						Name: "labels",
						Type: "object",
						AdditionalProperties: &parser.Property{
							Type: "object",
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

	assert.Contains(t, content, `selected = flattenSDKUnionsExceptUnder(selected, EventGatewayListenerPolicySDKOpsFreeformKeyFields, variantJSON)`)
	assert.Contains(t, content, `variantJSON = "tls_server"`)
	assert.NotContains(t, content, `selected = flattenSDKUnions(selected)`)
}

func TestFlattenSDKUnionsHelper_FlattensNonObjectMembers(t *testing.T) {
	t.Parallel()

	assert.Contains(t, flattenSDKUnionsHelper, `if len(x) == 2 {`)
	assert.Contains(t, flattenSDKUnionsHelper, `return inner`)
	assert.Contains(t, flattenSDKUnionsHelper, `func nestedSDKUnionMember(object map[string]any) (string, string, any, bool) {`)
	assert.Contains(t, flattenSDKUnionsHelper, `func nestedSDKUnionMemberForKey(object map[string]any, key string) (string, any, bool) {`)
	assert.Contains(t, flattenSDKUnionsHelper, `func flattenSDKUnionsExcept(v any, fields []sdkOpsFreeformKeyField) any {`)
	assert.Contains(t, flattenSDKUnionsHelper, `func flattenSDKUnionsExceptUnder(v any, fields []sdkOpsFreeformKeyField, variant string) any {`)
}
