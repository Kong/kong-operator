package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCACertificateOnKonnectGatewayControlPlane is the index field for KongCACertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCACertificateOnKonnectGatewayControlPlane = "kongCACertificateKonnectGatewayControlPlaneRef"
)

// OptionsForKongCACertificate returns required Index options for KongCACertificate reconciler.
func OptionsForKongCACertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCACertificate{},
			Field:          IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongCACertificate](cl),
		},
	}
}
