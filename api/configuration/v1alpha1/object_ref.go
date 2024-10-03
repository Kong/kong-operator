package v1alpha1

// KongObjectRef is a reference to another object representing a Kong entity with deterministic type.
//
// TODO: https://github.com/Kong/kubernetes-configuration/issues/96
// change other types to use the generic `KongObjectRef` and move it to a common package to prevent possible import cycles.
// +apireference:kgo:include
type KongObjectRef struct {
	// Name is the name of the entity.
	//
	// NOTE: the `Required` validation rule does not reject empty strings so we use `MinLength` to reject empty string here.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// TODO: handle cross namespace references.
}
