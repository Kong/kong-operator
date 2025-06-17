package v1alpha1

// NameRef is a reference to another object representing a Kong entity with deterministic type.
//
// +apireference:kgo:include
type NameRef struct {
	// Name is the name of the entity.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
