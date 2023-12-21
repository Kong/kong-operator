package op

// CreatedUpdatedOrNoop represents a result of an operation that can either:
// - create a resource
// - update a resource
// - do nothing
type CreatedUpdatedOrNoop string

const (
	// Created indicates that an operation resulted in creation of a resource.
	Created CreatedUpdatedOrNoop = "created"
	// Updated indicates that an operation resulted in an update of a resource.
	Updated CreatedUpdatedOrNoop = "updated"
	// Noop indicated that an operation did not perform any actions.
	Noop CreatedUpdatedOrNoop = "noop"
)
