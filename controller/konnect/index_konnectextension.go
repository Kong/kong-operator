package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// IndexFieldKonnectExtensionOnAPIAuthConfiguration is the index field for KonnectExtension -> APIAuthConfiguration.
	IndexFieldKonnectExtensionOnAPIAuthConfiguration = "konnectExtensionAPIAuthConfigurationRef"
	// IndexFieldKonnectExtensionOnSecrets is the index field for KonnectExtension -> Secret.
	IndexFieldKonnectExtensionOnSecrets = "konnectExtensionSecretRef"
)

// IndexOptionsForKonnectExtension returns required Index options for KonnectExtension reconciler.
func IndexOptionsForKonnectExtension() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &konnectv1alpha1.KonnectExtension{},
			IndexField:   IndexFieldKonnectExtensionOnAPIAuthConfiguration,
			ExtractValue: konnectExtensionAPIAuthConfigurationRef,
		},
		{
			IndexObject:  &konnectv1alpha1.KonnectExtension{},
			IndexField:   IndexFieldKonnectExtensionOnSecrets,
			ExtractValue: konnectExensionSecertRef,
		},
	}
}

func konnectExtensionAPIAuthConfigurationRef(object client.Object) []string {
	ext, ok := object.(*konnectv1alpha1.KonnectExtension)
	if !ok {
		return nil
	}

	return []string{ext.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
}

func konnectExensionSecertRef(obj client.Object) []string {
	ext, ok := obj.(*konnectv1alpha1.KonnectExtension)
	if !ok {
		return nil
	}

	if ext.Spec.DataPlaneClientAuth == nil ||
		ext.Spec.DataPlaneClientAuth.CertificateSecret.CertificateSecretRef == nil {
		return nil
	}

	return []string{ext.Spec.DataPlaneClientAuth.CertificateSecret.CertificateSecretRef.Name}
}
