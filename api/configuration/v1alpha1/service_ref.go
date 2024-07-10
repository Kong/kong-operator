package v1alpha1

const (
	ServiceRefNamespacedRef = "namespacedRef"
)

type ServiceRef struct {
	// Type can be one of:
	// - namespacedRef
	Type string `json:"type,omitempty"`

	NamespacedRef *NamespacedServiceRef `json:"namespacedRef,omitempty"`
}

type NamespacedServiceRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}
