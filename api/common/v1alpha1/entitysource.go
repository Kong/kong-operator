package v1alpha1

// EntitySource is the type for all the entity types.
type EntitySource string

const (
	// EntitySourceOrigin is the type for Origin entities.
	// Origin entities are the source of truth for the data.
	EntitySourceOrigin EntitySource = "Origin"
	// EntitySourceMirror is the type for Mirror entities.
	// Mirror entities are local mirrors of the remote konnect resource.
	EntitySourceMirror EntitySource = "Mirror"
)
