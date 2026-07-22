package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ObjectHasFinalizer returns a function that checks if the given object
// has the specified finalizer.
func ObjectHasFinalizer[
	T client.Object,
](finalizer string) func(obj T) bool {
	return func(obj T) bool {
		return controllerutil.ContainsFinalizer(obj, finalizer)
	}
}
