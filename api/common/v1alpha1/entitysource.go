package v1alpha1

// EntitySource is the type for all the entity types.
type EntitySource string

const (
	// EntityTypeOrigin is the type for Origin entities.
	// Origin entities are the source of truth for the data.
	EntityTypeOrigin EntitySource = "Origin"
	// EntityTypeMirror is the type for Mirror entities.
	// Mirror entities are local mirrors of the remote konnect resource.
	EntityTypeMirror EntitySource = "Mirror"
)
