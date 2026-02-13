package v1alpha2

// KonnectConfiguration is the Schema for the KonnectConfiguration API.
type KonnectConfiguration struct {
	// APIAuthConfigurationRef is the reference to the API Auth Configuration
	// that should be used for this Konnect Configuration.
	//
	// +required
	APIAuthConfigurationRef KonnectAPIAuthConfigurationRef `json:"authRef"`

	// NOTE: Place for extending the KonnectConfiguration object.
	// This is a good place to add fields like "class" which could reference a cluster-wide
	// configuration for Konnect (similar to what Gateway API's GatewayClass).
}

// ControlPlaneKonnectConfiguration is the Schema for the KonnectConfiguration API in the control plane.
type ControlPlaneKonnectConfiguration struct {
	// APIAuthConfigurationRef is the reference to the API Auth Configuration
	// that should be used for this Konnect Configuration.
	//
	// +required
	APIAuthConfigurationRef ControlPlaneKonnectAPIAuthConfigurationRef `json:"authRef"`

	// NOTE: Place for extending the ControlPlaneKonnectConfiguration object.
	// This is a good place to add fields like "class" which could reference a cluster-wide
	// configuration for Konnect (similar to what Gateway API's GatewayClass).
}
