package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

const (
	// IndexFieldKongCertificateOnKonnectGatewayControlPlane is the index field for KongCertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCertificateOnKonnectGatewayControlPlane = "kongCertificateKonnectGatewayControlPlaneRef"
)

// OptionsForKongCertificate returns required Index options for KongCertificate reconciler.
func OptionsForKongCertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCertificate{},
			Field:          IndexFieldKongCertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongCertificate](cl),
		},
	}
}
