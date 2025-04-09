package v1alpha1

import commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"

const (
	// ServiceRefNamespacedRef is a namespaced reference to a KongService.
	ServiceRefNamespacedRef = "namespacedRef"
)

// ServiceRef is a reference to a KongService.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true", message="when type is namespacedRef, namespacedRef must be set"
// +apireference:kgo:include
type ServiceRef struct {
	// Type can be one of:
	// - namespacedRef
	//
	// +kubebuilder:validation:Enum:=namespacedRef
	Type string `json:"type,omitempty"`

	// NamespacedRef is a reference to a KongService.
	NamespacedRef *commonv1alpha1.NameRef `json:"namespacedRef,omitempty"`
}
