package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongSNIOnCertificateRefNmae is the index field for KongSNI -> Certificate.
	IndexFieldKongSNIOnCertificateRefNmae = "kongSNICertificateRefName"
)

// OptionsForKongSNI returns required Index options for KongSNI reconciler.
func OptionsForKongSNI() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongSNI{},
			Field:          IndexFieldKongSNIOnCertificateRefNmae,
			ExtractValueFn: kongSNIReferencesCertificate,
		},
	}
}

func kongSNIReferencesCertificate(object client.Object) []string {
	sni, ok := object.(*configurationv1alpha1.KongSNI)
	if !ok {
		return nil
	}
	return []string{sni.Spec.CertificateRef.Name}
}
