package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func objectHasFinalizer[
	T client.Object,
](finalizer string) func(obj T) bool {
	return func(obj T) bool {
		return controllerutil.ContainsFinalizer(obj, finalizer)
	}
}
