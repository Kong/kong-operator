package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
			ExtractValueFn: SecretOnKongCertificate,
		},
	}
}

// SecretOnKongCertificate indexes KongCertificate by its referenced Secret.
func SecretOnKongCertificate(object client.Object) []string {
	cert, ok := object.(*configurationv1alpha1.KongCertificate)
	if !ok {
		return nil
	}

	var refs []string
	if cert.Spec.SecretRef != nil {
		ns := cert.Namespace
		if cert.Spec.SecretRef.Namespace != nil && *cert.Spec.SecretRef.Namespace != "" {
			ns = *cert.Spec.SecretRef.Namespace
		}
		refs = append(refs, ns+"/"+cert.Spec.SecretRef.Name)
	}

	if cert.Spec.SecretRefAlt != nil {
		ns := cert.Namespace
		if cert.Spec.SecretRefAlt.Namespace != nil && *cert.Spec.SecretRefAlt.Namespace != "" {
			ns = *cert.Spec.SecretRefAlt.Namespace
		}
		refs = append(refs, ns+"/"+cert.Spec.SecretRefAlt.Name)
	}

	return refs
}
