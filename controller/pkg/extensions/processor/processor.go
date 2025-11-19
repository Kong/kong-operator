package processor

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Processor defines how to process extensions for a specific resource type.
type Processor interface {
	// Process the extension for the given object.
	Process(ctx context.Context, cl client.Client, obj client.Object) (bool, error)
}
