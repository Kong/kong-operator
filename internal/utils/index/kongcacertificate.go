package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongCACertificateOnKonnectGatewayControlPlane is the index field for KongCACertificate -> KonnectGatewayControlPlane.
	IndexFieldKongCACertificateOnKonnectGatewayControlPlane = "kongCACertificateKonnectGatewayControlPlaneRef"
	// IndexFieldKongCACertificateReferencesSecrets is the index field for KongCACertificate -> Secret.
	IndexFieldKongCACertificateReferencesSecrets = "kongCACertificateSecretRef" // #nosec G101
)

// OptionsForKongCACertificate returns required Index options for KongCACertificate reconciler.
func OptionsForKongCACertificate(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongCACertificate{},
			Field:          IndexFieldKongCACertificateOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongCACertificate](cl),
		},
		{
			Object:         &configurationv1alpha1.KongCACertificate{},
			Field:          IndexFieldKongCACertificateReferencesSecrets,
			ExtractValueFn: SecretOnKongCACertificate,
		},
	}
}

// SecretOnKongCACertificate indexes KongCACertificate by its referenced Secret.
func SecretOnKongCACertificate(object client.Object) []string {
	cert, ok := object.(*configurationv1alpha1.KongCACertificate)
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

	return refs
}
