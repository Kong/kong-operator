package v1alpha1

import commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"

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
	// If namespace is not specified, the KongService in the same namespace
	// as the referencing entity.
	// Namespace can be specified to reference a KongService in a different namespace
	// but this requires a KongReferenceGrant in the target namespace allowing
	// the reference.
	//
	// +optional
	NamespacedRef *commonv1alpha1.NamespacedRef `json:"namespacedRef,omitempty"`
}
