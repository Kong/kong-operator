package multiinstance

import "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"

// InstanceWithIDAlreadyScheduledError is an error indicating that an instance with the same ID is already scheduled.
type InstanceWithIDAlreadyScheduledError struct {
	id manager.ID
}

// NewInstanceWithIDAlreadyScheduledError creates a new InstanceWithIDAlreadyScheduledError for the given ID.
func NewInstanceWithIDAlreadyScheduledError(id manager.ID) InstanceWithIDAlreadyScheduledError {
	return InstanceWithIDAlreadyScheduledError{id: id}
}

func (e InstanceWithIDAlreadyScheduledError) Error() string {
	return "instance with ID " + e.id.String() + " already exists"
}

// InstanceNotFoundError is an error indicating that an instance with the given ID was not found in the manager.
// It can indicate that the instance was never scheduled or was stopped.
type InstanceNotFoundError struct {
	id manager.ID
}

// NewInstanceNotFoundError creates a new InstanceNotFoundError for the given ID.
func NewInstanceNotFoundError(id manager.ID) InstanceNotFoundError {
	return InstanceNotFoundError{id: id}
}

func (e InstanceNotFoundError) Error() string {
	return "instance with ID " + e.id.String() + " not found"
}
