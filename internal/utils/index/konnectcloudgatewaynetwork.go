package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration is the index field for KonnectCloudGatewayNetwork -> APIAuthConfiguration.
	IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration = "konnectCloudGatewayNetworkAPIAuthConfigurationRef"
)

// OptionsForKonnectCloudGatewayNetwork returns required Index options for KonnectCloudGatewayNetwork reconciler.
func OptionsForKonnectCloudGatewayNetwork() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha1.KonnectCloudGatewayNetwork{},
			Field:          IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration,
			ExtractValueFn: konnectCloudGatewayNetworkAPIAuthConfigurationRef,
		},
	}
}

func konnectCloudGatewayNetworkAPIAuthConfigurationRef(object client.Object) []string {
	cp, ok := object.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
	if !ok {
		return nil
	}

	return []string{cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
}
