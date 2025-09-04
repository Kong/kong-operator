package converter

import (
	"context"

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
	// SetRootObject sets the root object to the provided value of type t.
	SetRootObject(obj t)
	// LoadInputStore loads and initializes any required internal store or cache, using the provided context.
	LoadInputStore(ctx context.Context) error
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
	*corev1.Service |
		*gwtypes.HTTPRoute

	client.Object
}
