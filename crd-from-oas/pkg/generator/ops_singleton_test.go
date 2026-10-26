package generator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestGenerateOpsUpdate_MultiParentSingleton(t *testing.T) {
	g := NewGenerator(Config{
		APIGroupPackagePath:  "github.com/kong/kong-operator/v2/api/configuration/v1alpha1",
		APIGroupPackageAlias: "configurationv1alpha1",
		ReconcilerConfig: map[string]*config.ReconcilerConfig{
			"EventGatewayVirtualClusterPolicyChain": {
				IsRoot: new(false),
				ParentEntityGVK: &config.EntityGVKConfig{
					Kind: "EventGatewayVirtualCluster",
				},
			},
		},
	})
	schema := &parser.Schema{
		OperationID:        "update-event-gateway-virtual-cluster-cluster-level-policy-chain",
		Tags:               []string{"Event Gateway Virtual Cluster Policies"},
		SuccessResponseRef: "EventGatewayPolicyChainResponse",
		Dependencies: []*parser.Dependency{
			{ParamName: "gatewayId", EntityName: "Gateway"},
			{ParamName: "virtualClusterId", EntityName: "VirtualCluster"},
		},
		UpdateOperationID: "update-event-gateway-virtual-cluster-cluster-level-policy-chain",
		UpdateTags:        []string{"Event Gateway Virtual Cluster Policies"},
		UpdatePathParams:  []string{"gatewayId", "virtualClusterId"},
	}
	opsConfig := &config.EntityOpsConfig{
		Ops: map[string]*config.OpConfig{
			"create": {Path: "github.com/Kong/sdk-konnect-go/models/operations.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest"},
			"update": {Path: "github.com/Kong/sdk-konnect-go/models/operations.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest"},
		},
	}
	res, err := g.generateEntityOpsFile("EventGatewayVirtualClusterPolicyChain", schema, opsConfig)
	require.NoError(t, err)
	require.NotNil(t, res.File)
	require.Contains(t, res.File.Content, "req.GatewayID = gatewayID")
	require.Contains(t, res.File.Content, "req.VirtualClusterID = virtualClusterID")
	require.NotContains(t, res.File.Content, "GetKonnectStatus().GetKonnectID()")
	require.NotContains(t, res.File.Content, "SetKonnectID")
	require.NotContains(t, res.File.Content, "req.VirtualClusterID = id")
	require.Contains(t, res.File.Content, "sdk.UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(ctx, *req)")
}
