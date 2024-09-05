package v1alpha1

const (
	// ServiceRefNamespacedRef is a namespaced reference to a KongService.
	ServiceRefNamespacedRef = "namespacedRef"
)

// ServiceRef is a reference to a KongService.
// +kubebuilder:validation:XValidation:rule="self.type == 'namespacedRef' ? has(self.namespacedRef) : true", message="when type is namespacedRef, namespacedRef must be set"
type ServiceRef struct {
	// Type can be one of:
	// - namespacedRef
	Type string `json:"type,omitempty"`

	// NamespacedRef is a reference to a KongService.
	NamespacedRef *NamespacedServiceRef `json:"namespacedRef,omitempty"`
}

type NamespacedServiceRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}
