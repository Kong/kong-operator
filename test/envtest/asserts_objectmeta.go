package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectMatchesName returns a function that checks if the given object has
// the same name as the provided object to match.
func ObjectMatchesName[
	T client.Object,
](objToMatch T) func(T) bool {
	return func(obj T) bool {
		return obj.GetName() == objToMatch.GetName()
	}
}
