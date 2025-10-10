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
	// Mode selects how the operator adopts an already-existing entity (for example,
	// a Konnect resource) instead of creating a new one.
	//
	// Supported values:
	// - "match": the operator retrieves the remote entity referenced by the
	//   corresponding Adopt* options (for example, adopt.konnect.id) and performs a
	//   field-by-field comparison against this CR's spec (ignoring server-assigned
	//   metadata). If the specification matches the remote state, the operator
	//   adopts the entity: it sets the status identifier and marks the resource as
	//   ready/programmed without issuing any write operation to the remote system.
	//   If the specification does not match the remote state, adoption fails: the
	//   operator does not modify the remote entity and surfaces a failure
	//   condition, allowing the user to align the spec with the existing entity if
	//   adoption is desired.
	//
	// Default: when unset, "match" is assumed.
	// +optional
	// +kubebuilder:validation:Enum=match
	Mode AdoptMode `json:"mode,omitempty"`
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

// AdoptMode is the strategy used when adopting an existing entity.
//
// The set of supported modes may be extended in the future. At present the
// only value is "match", which requires exact (semantically equivalent)
// alignment between the CR spec and the remote entity before adoption.
// No mutations are performed against the remote system during adoption.
// If any relevant field differs, adoption fails and the operator will not
// take ownership until the spec is aligned.
type AdoptMode string

const (
	// AdoptModeMatch enforces read-only adoption: the operator will only adopt
	// the remote entity when the CR spec matches the remote configuration; no
	// write operations are issued to the remote system during adoption.
	AdoptModeMatch AdoptMode = "match"
)

// AdoptKonnectOptions specifies the options for adopting the entity from Konnect.
// +kubebuilder:object:generate=true
// +apireference:kgo:include
type AdoptKonnectOptions struct {
	// ID is the Konnect ID of the entity.
	// +required
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
}
