package v1beta1

import (
	extcommonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	localcommonv1alpha1 "github.com/kong/kong-operator/apis/common/v1alpha1"
)

// convertExtensionRefsToExternal converts local ExtensionRefs to external ExtensionRefs
func convertExtensionRefsToExternal(local []localcommonv1alpha1.ExtensionRef) []extcommonv1alpha1.ExtensionRef {
	external := make([]extcommonv1alpha1.ExtensionRef, len(local))
	for i, ref := range local {
		external[i] = extcommonv1alpha1.ExtensionRef{
			Group: ref.Group,
			Kind:  ref.Kind,
			NamespacedRef: extcommonv1alpha1.NamespacedRef{
				Name:      ref.Name,
				Namespace: ref.Namespace,
			},
		}
	}
	return external
}

// convertExtensionRefsFromExternal converts external ExtensionRefs to local ExtensionRefs
func convertExtensionRefsFromExternal(external []extcommonv1alpha1.ExtensionRef) []localcommonv1alpha1.ExtensionRef {
	local := make([]localcommonv1alpha1.ExtensionRef, len(external))
	for i, ref := range external {
		local[i] = localcommonv1alpha1.ExtensionRef{
			Group: ref.Group,
			Kind:  ref.Kind,
			NamespacedRef: localcommonv1alpha1.NamespacedRef{
				Name:      ref.Name,
				Namespace: ref.Namespace,
			},
		}
	}
	return local
}
