package index

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Option contains the information needed to set up an index for a field on an object.
type Option struct {
	// Object is the object type to index.
	Object client.Object

	// Field is the name of the index to create.
	Field string

	// ExtractValueFn is a function that extracts the value to index on from the object.
	ExtractValueFn client.IndexerFunc
}

// String returns a string representation of the Option.
func (e Option) String() string {
	return fmt.Sprintf("%T[%s]", e.Object, e.Field)
}
