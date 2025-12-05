package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCertificateOnKonnectGatewayControlPlane is the index field for KongCertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCertificateOnKonnectGatewayControlPlane = "kongCertificateKonnectGatewayControlPlaneRef"
	// IndexFieldKongCertificateReferencesSecrets is the index field for KongCertificate -> Secret.
	IndexFieldKongCertificateReferencesSecrets = "kongCertificateSecretRef"
)

// OptionsForKongCertificate returns required Index options for KongCertificate reconciler.
func OptionsForKongCertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCertificate{},
			Field:          IndexFieldKongCertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongCertificate](cl),
		},
		{
			Object:         &configurationv1alpha1.KongCertificate{},
			Field:          IndexFieldKongCertificateReferencesSecrets,
			ExtractValueFn: secretOnKongCertificate,
		},
	}
}

// secretOnKongCertificate indexes KongCertificate by its referenced Secret.
func secretOnKongCertificate(object client.Object) []string {
	cert, ok := object.(*configurationv1alpha1.KongCertificate)
	if !ok {
		return nil
	}

	if cert.Spec.SecretRef == nil {
		return nil
	}

	return []string{cert.Spec.SecretRef.Namespace + "/" + cert.Spec.SecretRef.Name}
}
