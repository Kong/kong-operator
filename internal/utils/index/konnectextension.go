package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

const (
	// IndexFieldKonnectExtensionOnAPIAuthConfiguration is the index field for KonnectExtension -> APIAuthConfiguration.
	IndexFieldKonnectExtensionOnAPIAuthConfiguration = "konnectExtensionAPIAuthConfigurationRef"
	// IndexFieldKonnectExtensionOnSecrets is the index field for KonnectExtension -> Secret.
	IndexFieldKonnectExtensionOnSecrets = "konnectExtensionSecretRef"
	// IndexFieldKonnectExtensionOnKonnectGatewayControlPlane is the index field for KonnectExtension -> KonnectGatewayControlPlane.
	IndexFieldKonnectExtensionOnKonnectGatewayControlPlane = "konnectExtensionKonnectGatewayControlPlaneRef"
)

// OptionsForKonnectExtension returns required Index options for KonnectExtension reconciler.
func OptionsForKonnectExtension() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha2.KonnectExtension{},
			Field:          IndexFieldKonnectExtensionOnSecrets,
			ExtractValueFn: konnectExtensionSecretRef,
		},
		{
			Object:         &konnectv1alpha2.KonnectExtension{},
			Field:          IndexFieldKonnectExtensionOnKonnectGatewayControlPlane,
			ExtractValueFn: konnectExtensionControlPlaneRef,
		},
		{
			Object:         &konnectv1alpha2.KonnectExtension{},
			Field:          IndexFieldKonnectExtensionOnAPIAuthConfiguration,
			ExtractValueFn: konnectExtensionAPIAuthConfigurationRef,
		},
	}
}

func konnectExtensionSecretRef(obj client.Object) []string {
	ext, ok := obj.(*konnectv1alpha2.KonnectExtension)
	if !ok {
		return nil
	}

	if ext.Spec.ClientAuth == nil ||
		ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef == nil {
		return nil
	}

	return []string{ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name}
}

func konnectExtensionControlPlaneRef(obj client.Object) []string {
	ext, ok := obj.(*konnectv1alpha2.KonnectExtension)
	if !ok {
		return nil
	}

	if ext.Spec.Konnect.ControlPlane.Ref.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef {
		return nil
	}
	// TODO: add namespace to index when cross namespace reference is supported.
	return []string{ext.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef.Name}
}

func konnectExtensionAPIAuthConfigurationRef(obj client.Object) []string {
	ext, ok := obj.(*konnectv1alpha2.KonnectExtension)
	if !ok {
		return nil
	}

	if ext.GetKonnectAPIAuthConfigurationRef().Name == "" {
		return nil
	}

	return []string{ext.GetKonnectAPIAuthConfigurationRef().Name}
}
