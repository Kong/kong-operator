package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration is the index field for KonnectGatewayControlPlane -> APIAuthConfiguration.
	IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration = "konnectGatewayControlPlaneAPIAuthConfigurationRef"
)

// IndexOptionsForKonnectGatewayControlPlane returns required Index options for KonnectGatewayControlPlane reconciler.
func IndexOptionsForKonnectGatewayControlPlane() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &konnectv1alpha1.KonnectGatewayControlPlane{},
			IndexField:   IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration,
			ExtractValue: konnectGatewayControlPlaneAPIAuthConfigurationRef,
		},
	}
}


func konnectGatewayControlPlaneAPIAuthConfigurationRef(object client.Object) []string {
	cp, ok := object.(*konnectv1alpha1.KonnectGatewayControlPlane)
	if !ok {
		return nil
	}

	return []string{cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
}
