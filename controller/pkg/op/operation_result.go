package op

// Result represents a result of an operation that can either:
// - create a resource
// - update a resource
// - do nothing.
type Result string

const (
	// Created indicates that an operation resulted in creation of a resource.
	Created Result = "created"
	// Updated indicates that an operation resulted in an update of a resource.
	Updated Result = "updated"
	// Deleted indicates that an operation resulted in a delete of a resource.
	Deleted Result = "deleted"
	// Noop indicated that an operation did not perform any actions.
	Noop Result = "noop"
)
