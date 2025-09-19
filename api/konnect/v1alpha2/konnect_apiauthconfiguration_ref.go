package v1alpha2

// KonnectAPIAuthConfigurationRef is a reference to a KonnectAPIAuthConfiguration resource.
// +apireference:kgo:include
type KonnectAPIAuthConfigurationRef struct {
	// Name is the name of the KonnectAPIAuthConfiguration resource.
	// +required
	Name string `json:"name"`
}
