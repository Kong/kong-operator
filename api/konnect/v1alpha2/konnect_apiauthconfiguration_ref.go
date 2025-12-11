package v1alpha2

// KonnectAPIAuthConfigurationRef is a reference to a KonnectAPIAuthConfiguration resource.
// +apireference:kgo:include
type KonnectAPIAuthConfigurationRef struct {
	// Name is the name of the KonnectAPIAuthConfiguration resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ControlPlaneKonnectAPIAuthConfigurationRef is a reference to a KonnectAPIAuthConfiguration resource
// in the control plane.
// +apireference:kgo:include
type ControlPlaneKonnectAPIAuthConfigurationRef struct {
	// Name is the name of the KonnectAPIAuthConfiguration resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the KonnectAPIAuthConfiguration resource.
	// If not specified, defaults to the same namespace as the KonnectConfiguration resource.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	Namespace *string `json:"namespace,omitempty"`
}
