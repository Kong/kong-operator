package v1alpha1

// NameRef is a reference to another object representing a Kong entity with deterministic type.
type NameRef struct {
	// Name is the name of the entity.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}
