package konnect

import (
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

// KonnectExtensionToExtensionRef converts a KonnectExtension to the corresponding ExtensionRef.
func KonnectExtensionToExtensionRef(
	ns *string,
	extension *konnectv1alpha2.KonnectExtension,
) *commonv1alpha1.ExtensionRef {
	if extension == nil {
		return nil
	}
	return &commonv1alpha1.ExtensionRef{
		Group: konnectv1alpha2.SchemeGroupVersion.Group,
		Kind:  konnectv1alpha2.KonnectExtensionKind,
		NamespacedRef: commonv1alpha1.NamespacedRef{
			Name:      extension.Name,
			Namespace: ns,
		},
	}
}
