package index

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

const (
	// IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane is the index field for KongDataPlaneCertificate -> KonnectGatewayControlPlane.
	IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane = "dataPlaneCertificateKonnectGatewayControlPlaneRef"

	// IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner is the index field for KongDataPlaneCertificate -> KonnectExtension via the ownership.
	IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner = "dataPlaneCertificateKonnectExtensionOwner"
)

// OptionsForKongDataPlaneCertificate returns required Index options for KongConsumer reconciler.
func OptionsForKongDataPlaneCertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongDataPlaneClientCertificate{},
			Field:          IndexFieldKongDataPlaneClientCertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongDataPlaneClientCertificate](cl),
		},
		{
			Object:         &configurationv1alpha1.KongDataPlaneClientCertificate{},
			Field:          IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner,
			ExtractValueFn: kongDataPlaneCertificateOnKonnectExtensionOwner,
		},
	}
}

func kongDataPlaneCertificateOnKonnectExtensionOwner(object client.Object) []string {
	dpCert, ok := object.(*configurationv1alpha1.KongDataPlaneClientCertificate)

	if !ok {
		return nil
	}

	owners := dpCert.GetOwnerReferences()
	var ownerNames []string
	for _, owner := range owners {
		if owner.Kind != "KonnectExtension" {
			continue
		}
		if !strings.HasPrefix(owner.APIVersion, konnectv1alpha2.GroupVersion.Group) {
			continue
		}
		ownerNames = append(ownerNames, owner.Name)
	}
	return ownerNames
}
