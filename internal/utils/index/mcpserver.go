package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

const (
	// IndexFieldMCPServerOnKonnectGatewayControlPlane is the index field for MCPServer -> KonnectGatewayControlPlane.
	IndexFieldMCPServerOnKonnectGatewayControlPlane = "mcpServerKonnectGatewayControlPlaneRef"
)

// OptionsForMCPServer returns required Index options for MCPServer reconciler.
func OptionsForMCPServer(cl client.Client) []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.MCPServer{},
			Field:          IndexFieldMCPServerOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[konnectv1alpha1.MCPServer](cl),
		},
	}
}
