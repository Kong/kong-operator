package index

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

// OptionsForKonnectCloudGatewayDataPlaneGroupConfiguration returns required Index options for KonnectCloudGatewayDataPlaneGroupConfiguration reconciler.
func OptionsForKonnectCloudGatewayDataPlaneGroupConfiguration(cl client.Client) []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{},
			Field:          IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](cl),
		},
	}
}
