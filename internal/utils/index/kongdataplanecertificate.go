package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane is the index field for KongDataPlaneCertificate -> KonnectGatewayControlPlane.
	IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane = "dataPlaneCertificateKonnectGatewayControlPlaneRef"
)

// OptionsForKongDataPlaneCertificate returns required Index options for KongConsumer reconciler.
func OptionsForKongDataPlaneCertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongDataPlaneClientCertificate{},
			Field:          IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongDataPlaneClientCertificate](cl),
		},
	}
}
