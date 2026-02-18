package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongSNIOnCertificateRefName is the index field for KongSNI -> Certificate.
	IndexFieldKongSNIOnCertificateRefName = "kongSNICertificateRefName"
)

// OptionsForKongSNI returns required Index options for KongSNI reconciler.
func OptionsForKongSNI() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongSNI{},
			Field:          IndexFieldKongSNIOnCertificateRefName,
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
