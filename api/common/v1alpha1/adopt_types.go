package v1alpha1

// AdoptOptions is the options for CRDs to attach to an existing Kong entity.
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.from == oldSelf.from",message="'from'(adopt source) is immutable"
// +kubebuilder:validation:XValidation:rule="self.from == 'konnect' ? has(self.konnect) : true",message="Must specify Konnect options when from='konnect'"
// +kubebuilder:validation:XValidation:rule="has(self.konnect) ? (self.konnect.id == oldSelf.konnect.id) : true",message="konnect.id is immutable"
// +apireference:kgo:include
type AdoptOptions struct {
	// From is the source of the entity to adopt from.
	// Now 'konnect' is supported.
	// +required
	// +kubebuilder:validation:Enum=konnect
	From AdoptSource `json:"from"`
	// Konnect is the options for adopting the entity from Konnect.
	// Required when from == 'konnect'.
	// +optional
	Konnect *AdoptKonnectOptions `json:"konnect,omitempty"`
}

// AdoptSource is the type to define the source of the entity to adopt from.
type AdoptSource string

const (
	// AdoptSourceKonnect indicates that the entity is adopted from Konnect.
	AdoptSourceKonnect AdoptSource = "konnect"
)

// AdoptKonnectOptions specifies the options for adopting the entity from Konnect.
// +kubebuilder:object:generate=true
// +apireference:kgo:include
type AdoptKonnectOptions struct {
	// ID is the Konnect ID of the entity.
	// +required
	ID string `json:"id"`
}
