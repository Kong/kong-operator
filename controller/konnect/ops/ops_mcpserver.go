package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// ensureMCPServer ensures that the MCPServer exists in Konnect by fetching it
// by ID. Since MCPServer is mirror-only, Konnect is the source of truth.
func ensureMCPServer(
	ctx context.Context,
	sdk *sdkkonnectgo.MCPServers,
	mcpServer *konnectv1alpha1.MCPServer,
) error {
	mcpServerID := string(mcpServer.Spec.Mirror.Konnect.ID)
	controlPlaneID := mcpServer.GetControlPlaneID()

	resp, err := sdk.GetMcpServerByControlPlane(ctx, controlPlaneID, mcpServerID)
	if err != nil {
		return fmt.Errorf("failed getting %s %s: %w",
			mcpServer.GetTypeName(), client.ObjectKeyFromObject(mcpServer), err)
	}
	if resp == nil || resp.MCPServerCPInfo == nil {
		return fmt.Errorf("failed getting %s %s: %w",
			mcpServer.GetTypeName(), client.ObjectKeyFromObject(mcpServer), ErrNilResponse)
	}

	mcpServer.SetKonnectID(resp.MCPServerCPInfo.ID)
	return nil
}
