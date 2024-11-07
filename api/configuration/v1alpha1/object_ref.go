package v1alpha1

// TODO: https://github.com/Kong/kubernetes-configuration/issues/96
// Change other types to use the generic `KongObjectRef` and move it to a common package
// to prevent possible import cycles.

// KongObjectRef is a reference to another object representing a Kong entity with deterministic type.
//
// +apireference:kgo:include
type KongObjectRef struct {
	// Name is the name of the entity.
	//
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// TODO: handle cross namespace references.
}
