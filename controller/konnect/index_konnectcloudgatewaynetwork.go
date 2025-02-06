package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration is the index field for KonnectCloudGatewayNetwork -> APIAuthConfiguration.
	IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration = "konnectCloudGatewayNetworkAPIAuthConfigurationRef"
)

// IndexOptionsForKonnectCloudGatewayNetwork returns required Index options for KonnectCloudGatewayNetwork reconciler.
func IndexOptionsForKonnectCloudGatewayNetwork() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &konnectv1alpha1.KonnectCloudGatewayNetwork{},
			IndexField:   IndexFieldKonnectCloudGatewayNetworkOnAPIAuthConfiguration,
			ExtractValue: konnectCloudGatewayNetworkAPIAuthConfigurationRef,
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
