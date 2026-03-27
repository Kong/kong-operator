package mcpserver

import konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"

// ownerControlPlaneName returns the name of the KonnectGatewayControlPlane that
// owns the given MCPServer, or an empty string if no such owner is found.
func ownerControlPlaneName(mcpServer *konnectv1alpha1.MCPServer) string {
	for _, ref := range mcpServer.OwnerReferences {
		if ref.APIVersion == konnectv1alpha1.GroupVersion.String() && ref.Kind == "KonnectGatewayControlPlane" {
			return ref.Name
		}
	}
	return ""
}
