package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCertificateOnKonnectGatewayControlPlane is the index field for KongCertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCertificateOnKonnectGatewayControlPlane = "kongCertificateKonnectGatewayControlPlaneRef"
)

// IndexOptionsForKongCertificate returns required Index options for KongCertificate reconclier.
func IndexOptionsForKongCertificate() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongCertificate{},
			IndexField:   IndexFieldKongCertificateOnKonnectGatewayControlPlane,
			ExtractValue: konnectGatewayControlPlaneRefFromKongCertificate,
		},
	}
}

// konnectGatewayControlPlaneRefFromKongCertificate returns namespace/name of referenced KonnectGatewayControlPlane in KongCertificate spec.
func konnectGatewayControlPlaneRefFromKongCertificate(obj client.Object) []string {
	cert, ok := obj.(*configurationv1alpha1.KongCertificate)
	if !ok {
		return nil
	}
	return controlPlaneKonnectNamespacedRefAsSlice(cert)
}
