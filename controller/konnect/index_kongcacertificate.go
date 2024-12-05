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
func IndexOptionsForKongCACertificate() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongCACertificate{},
			IndexField:   IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
			ExtractValue: konnectGatewayControlPlaneRefFromKongCACertificate,
		},
	}
}

// konnectGatewayControlPlaneRefFromKongCACertificate returns namespace/name of referenced KonnectGatewayControlPlane in KongCACertificate spec.
func konnectGatewayControlPlaneRefFromKongCACertificate(obj client.Object) []string {
	cert, ok := obj.(*configurationv1alpha1.KongCACertificate)
	if !ok {
		return nil
	}
	return controlPlaneKonnectNamespacedRefAsSlice(cert)
}
