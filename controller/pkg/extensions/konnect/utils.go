package konnect

import (
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// KonnectExtensionToExtensionRef converts a KonnectExtension to the corresponding ExtensionRef.
func KonnectExtensionToExtensionRef(extension *konnectv1alpha2.KonnectExtension) *commonv1alpha1.ExtensionRef {
	if extension == nil {
		return nil
	}
	return &commonv1alpha1.ExtensionRef{
		Group: konnectv1alpha2.SchemeGroupVersion.Group,
		Kind:  konnectv1alpha2.KonnectExtensionKind,
		NamespacedRef: commonv1alpha1.NamespacedRef{
			Name:      extension.Name,
			Namespace: lo.ToPtr(extension.Namespace),
		},
	}
}
