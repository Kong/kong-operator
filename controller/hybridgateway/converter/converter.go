package converter

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// APIConverter defines an interface for converting and managing Kubernetes API objects
// of a generic type t that satisfies the RootObject constraint.
type APIConverter[t RootObject] interface {
	// GetExpectedGVKs returns the list of GroupVersionKinds for resources expected to be created by this converter.
	// The order of the returned slice is preserved during cleanup operations.
	GetExpectedGVKs() []schema.GroupVersionKind
	// Translate performs the conversion or translation logic for the root object, returning the number of Kong resources translated and an error if the process fails.
	Translate(ctx context.Context, logger logr.Logger) (int, error)
	// GetRootObject returns the current root object of type t.
	GetRootObject() t
	// GetOutputStore returns a slice of unstructured.Unstructured objects representing the current state of the store, using the provided context.
	GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error)
	// UpdateRootObjectStatus updates the status for the root object.
	UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error)
	// HandleOrphanedResource is called before deleting orphaned resources to allow the converter to perform custom cleanup logic and skip the deletion.
}

// OrphanedResourceHandler is an optional interface that converters can implement
// to provide custom cleanup logic for orphaned resources. If a converter implements
// this interface, the HandleOrphanedResource method will be called before deleting
// orphaned resources, allowing the converter to perform custom logic such as
// updating the resource or deciding whether to skip deletion.
type OrphanedResourceHandler interface {
	// HandleOrphanedResource is called before deleting orphaned resources.
	// Returns:
	//   - skipDelete: true if the resource should not be deleted, false to proceed with deletion
	//   - err: any error that occurred during processing
	HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error)
}

// RootObject is an interface that represents all resource types that can be loaded
// as root by the APIConverter.
type RootObject interface {
	gwtypes.HTTPRoute | gwtypes.Gateway
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
func NewConverter[t RootObject](obj t, cl client.Client, fqdnMode bool, clusterDomain string) (APIConverter[t], error) {
	switch o := any(obj).(type) {
	// TODO: add other types here
	case gwtypes.HTTPRoute:
		return newHTTPRouteConverter(&o, cl, fqdnMode, clusterDomain).(APIConverter[t]), nil
	case gwtypes.Gateway:
		return newGatewayConverter(&o, cl).(APIConverter[t]), nil
	default:
		return nil, fmt.Errorf("unsupported root object type: %T", obj)
	}
}
