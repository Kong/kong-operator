package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnAPIAuthConfiguration is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> APIAuthConfiguration.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnAPIAuthConfiguration = "konnectCloudGatewayDataPlaneGroupConfigurationAPIAuthConfigurationRef"
	// IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane is the index field for KonnectCloudGatewayDataPlaneGroupConfiguration -> KonnectGatewayControlPlane.
	IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane = "konnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKonnectCloudGatewayDataPlaneGroupConfiguration returns required Index options for KonnectCloudGatewayDataPlaneGroupConfiguration reconciler.
func IndexOptionsForKonnectCloudGatewayDataPlaneGroupConfiguration(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			IndexField:   IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](cl),
		},
	}
}
