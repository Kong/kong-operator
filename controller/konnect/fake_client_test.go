package konnect

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newFakeClientBuilder returns a fake client builder whose plain object tracker
// skips client-go's per-call static REST mapper rebuild, which otherwise
// dominates the cost of the suite. Apply does not work over it: none of the
// tests here apply objects.
func newFakeClientBuilder(sm *runtime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(sm).
		WithObjectTracker(k8stesting.NewObjectTracker(sm, serializer.NewCodecFactory(sm).UniversalDecoder()))
}
