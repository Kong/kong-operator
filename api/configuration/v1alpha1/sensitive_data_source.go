package v1alpha1

// SensitiveDataSecretRef identifies a specific key inside a Kubernetes Secret
// that holds a sensitive value for a CRD field.
type SensitiveDataSecretRef struct {
	// Name is the name of the referred resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitzero"`

	// Key is the data key within the Secret. When omitted, some fields fall
	// back to a fixed default key documented on the parent field; others
	// require it to be set.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key,omitzero"`

	// Namespace is the namespace of the referred resource.
	//
	// For namespace-scoped resources if no Namespace is provided then the
	// namespace of the parent object MUST be used.
	//
	// This field MUST not be set when referring to cluster-scoped resources.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Namespace *string `json:"namespace,omitempty"`
}
