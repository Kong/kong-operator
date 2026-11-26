package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func skipUnchangedTestGenerator(parentEntityType string) *Generator {
	return NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",
		APIGroupPackageAlias: "aiconfigurationv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"AIGatewayConsumer": {
				IsRoot:           new(false),
				ParentEntityType: parentEntityType,
			},
		},
	})
}

// skipUnchangedTestSchema returns the schema of an AI Gateway consumer:
// PUT and GET /v1/ai-gateways/{gatewayId}/consumers/{consumerIdOrName}.
func skipUnchangedTestSchema() *parser.Schema {
	return &parser.Schema{
		OperationID:        "create-ai-gateway-consumer",
		Tags:               []string{"AI Gateway Consumers"},
		SuccessResponseRef: "AIGatewayConsumer",
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "Gateway"},
		},
		UpdateOperationID:        "update-ai-gateway-consumer",
		UpdateTags:               []string{"AI Gateway Consumers"},
		UpdatePathParams:         []string{"gatewayId", "consumerIdOrName"},
		UpdateSuccessResponseRef: "AIGatewayConsumer",
		GetOperationID:           "get-ai-gateway-consumer",
		GetTags:                  []string{"AI Gateway Consumers"},
		GetPathParams:            []string{"gatewayId", "consumerIdOrName"},
		GetSuccessResponseRef:    "AIGatewayConsumer",
	}
}

func skipUnchangedTestOpsConfig() *config.EntityOpsConfig {
	return &config.EntityOpsConfig{
		RequireClient:        true,
		SkipUnchangedUpdates: true,
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/components.CreateAIGatewayConsumerRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/components.UpdateAIGatewayConsumerRequest"},
		},
	}
}

func TestGenerateOpsUpdate_SkipUnchanged(t *testing.T) {
	g := skipUnchangedTestGenerator("KonnectAIGateway")

	file, info, err := g.generateOpsUpdate("AIGatewayConsumer", skipUnchangedTestSchema(), skipUnchangedTestOpsConfig())
	require.NoError(t, err)
	require.NotNil(t, file)
	require.NotNil(t, info)

	assert.True(t, info.SkipUnchanged)
	assert.Equal(t, "GetGatewayID", info.ParentIDGetter)

	assert.Contains(t, file.Content, "g *syncGuard,")
	assert.Contains(t, file.Content, `sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"`)
	assert.Contains(t, file.Content, "payload := req")
	assert.Contains(t, file.Content, "err = upsertUnlessInSync(ctx, g, id, payload,")
	assert.Contains(t, file.Content, "func(ctx context.Context) (*sdkkonnectcomp.AIGatewayConsumer, error) {")
	assert.Contains(t, file.Content, "resp, err := sdk.GetAiGatewayConsumer(ctx, parentID, id)")
	assert.Contains(t, file.Content, "resp, err := sdk.UpdateAiGatewayConsumer(ctx, sdkkonnectops.UpdateAiGatewayConsumerRequest{")
	assert.Contains(t, file.Content, "return resp.AIGatewayConsumer, nil")
	assert.NotContains(t, file.Content, "_, err = sdk.UpdateAiGatewayConsumer(", "the unguarded call must not be emitted")
}

func TestGenerateOpsUpdate_WithoutSkipUnchanged(t *testing.T) {
	g := skipUnchangedTestGenerator("KonnectAIGateway")
	opsConfig := skipUnchangedTestOpsConfig()
	opsConfig.SkipUnchangedUpdates = false

	file, info, err := g.generateOpsUpdate("AIGatewayConsumer", skipUnchangedTestSchema(), opsConfig)
	require.NoError(t, err)
	assert.False(t, info.SkipUnchanged)
	assert.NotContains(t, file.Content, "syncGuard")
	assert.NotContains(t, file.Content, "upsertUnlessInSync")
	assert.Contains(t, file.Content, "_, err = sdk.UpdateAiGatewayConsumer(")
}

func TestGenerateOpsUpdate_SkipUnchangedValidation(t *testing.T) {
	testCases := []struct {
		name          string
		parent        string
		mutateSchema  func(*parser.Schema)
		mutateConfig  func(*config.EntityOpsConfig)
		expectedError string
	}{
		{
			name:          "parent without an expected config version",
			parent:        "KonnectEventGateway",
			expectedError: "requires a single KonnectAIGateway parent",
		},
		{
			name:   "no GET operation",
			parent: "KonnectAIGateway",
			mutateSchema: func(s *parser.Schema) {
				s.GetOperationID = ""
				s.GetPathParams = nil
			},
			expectedError: "requires a GET operation keyed by parent and entity ID",
		},
		{
			name:   "GET and update return different schemas",
			parent: "KonnectAIGateway",
			mutateSchema: func(s *parser.Schema) {
				s.GetSuccessResponseRef = "AIGatewayConsumerWithSecrets"
			},
			expectedError: "requires the GET and update operations to return the same schema",
		},
		{
			name:   "response status fields",
			parent: "KonnectAIGateway",
			mutateConfig: func(c *config.EntityOpsConfig) {
				c.ResponseStatusFields = []config.ResponseStatusFieldConfig{{StatusField: "Endpoint", RespPath: "Endpoint"}}
			},
			expectedError: "does not support ops.responseStatusFields",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			schema := skipUnchangedTestSchema()
			if tc.mutateSchema != nil {
				tc.mutateSchema(schema)
			}
			opsConfig := skipUnchangedTestOpsConfig()
			if tc.mutateConfig != nil {
				tc.mutateConfig(opsConfig)
			}
			_, _, err := skipUnchangedTestGenerator(tc.parent).generateOpsUpdate("AIGatewayConsumer", schema, opsConfig)
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}

func TestGenerateOpsUpdateDispatcher_SkipUnchanged(t *testing.T) {
	infos := []*OpsUpdateFileInfo{
		{
			Entity:         "AIGatewayConsumer",
			APIAlias:       "aiconfigurationv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",
			SDKGetter:      "GetAIGatewayConsumersSDK",
			NeedsClient:    true,
			SkipUnchanged:  true,
			ParentIDGetter: "GetGatewayID",
		},
		{
			Entity:         "Portal",
			APIAlias:       "konnectv1alpha1",
			APIPackagePath: "github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
			SDKGetter:      "GetPortalsSDK",
		},
	}

	file, err := GenerateOpsUpdateDispatcher(infos)
	require.NoError(t, err)

	assert.Contains(t, file.Content, "g *syncGuard,")
	assert.Contains(t, file.Content, "return updateAIGatewayConsumer(ctx, cl, sdk.GetAIGatewayConsumersSDK(), g, ent)")
	assert.Contains(t, file.Content, "return updatePortal(ctx, sdk.GetPortalsSDK(), ent)")
	assert.Contains(t, file.Content, "func generatedAIGatewaySyncScope(e any) (string, bool) {")
	assert.Contains(t, file.Content, "return e.(*aiconfigurationv1alpha1.AIGatewayConsumer).GetGatewayID(), true")
	assert.NotContains(t, file.Content, "e.(*konnectv1alpha1.Portal)", "only guarded entities have a sync scope")
}
