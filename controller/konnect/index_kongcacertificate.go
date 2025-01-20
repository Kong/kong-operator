package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCACertificateOnKonnectGatewayControlPlane is the index field for KongCACertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCACertificateOnKonnectGatewayControlPlane = "kongCACertificateKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongCACertificate returns required Index options for KongCACertificate reconclier.
func IndexOptionsForKongCACertificate(cl client.Client) []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongCACertificate{},
			IndexField:   IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
			ExtractValue: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongCACertificate](cl),
		},
	}
}
