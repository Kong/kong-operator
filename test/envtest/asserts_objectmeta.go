package envtest

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func objectMatchesName[
	T client.Object,
](objToMatch T) func(T) bool {
	return func(obj T) bool {
		return obj.GetName() == objToMatch.GetName()
	}
}
