package v1alpha1

// +kubebuilder:object:generate=false
type KonnectConfiguration struct {
	// APIAuthConfigurationRef is the reference to the API Auth Configuration
	// that should be used for this Konnect Configuration.
	//
	// +kubebuilder:validation:Required
	APIAuthConfigurationRef KonnectAPIAuthConfigurationRef `json:"authRef"`

	// NOTE: Place for extending the KonnectConfiguration object.
	// This is a good place to add fields like "class" which could reference a cluster-wide
	// configuration for Konnect (similar to what Gateway API's GatewayClass).
}
