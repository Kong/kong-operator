package converter

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/fullhybrid/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// APIConverter defines an interface for converting and managing Kubernetes API objects
// of a generic type t that satisfies the RootObject constraint.
type APIConverter[t RootObject] interface {
	// GetRootObject returns the current root object of type t.
	GetRootObject() t
	// Translate performs the conversion or translation logic for the root object, returning an error if the process fails.
	Translate() error
	// GetOutputStore returns a slice of unstructured.Unstructured objects representing the current state of the store, using the provided context.
	GetOutputStore(ctx context.Context) []unstructured.Unstructured
	// Reduce returns a slice of utils.ReduceFunc functions that can be applied to the given unstructured.Unstructured object to get a list of duplicates to be removed.
	Reduce(obj unstructured.Unstructured) []utils.ReduceFunc
	// ListExistingObjects lists all existing unstructured.Unstructured objects of the destination API kind, using the provided context, and returns them along with any error encountered.
	ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error)
}

// RootObject is an interface that represents all resource types that can be loaded
// as root by the APIConverter.
type RootObject interface {
	corev1.Service |
		gwtypes.HTTPRoute
}

// RootObjectPtr is a generic interface that represents a pointer to a type T,
// where T implements the RootObject interface. It also requires that the type
// implements the client.Object interface, enabling Kubernetes client operations.
type RootObjectPtr[T RootObject] interface {
	*T
	client.Object
}

// NewConverter is a factory function that creates and returns an APIConverter instance
// based on the type of the provided root object. It supports different types of root objects
// and returns an error if the type is unsupported.
func NewConverter[t RootObject](obj t, cl client.Client) (APIConverter[t], error) {
	switch o := any(obj).(type) {
	// TODO: add other types here
	case corev1.Service:
		return newServiceConverter(&o, cl).(APIConverter[t]), nil
	default:
		return nil, fmt.Errorf("unsupported root object type: %T", obj)
	}
}
