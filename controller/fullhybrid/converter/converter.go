package converter

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// APIConverter is an interface that groups the methods needed to convert a
// Kubernetes API object into Kong configuration objects.
type APIConverter[t RootObject] interface {
	RootLoader[t]
	StoreLoader
	Translator
}

// RootObject is an interface that represents all resource types that can be loaded
// as root by the APIConverter.
type RootObject interface {
	corev1.Service |
		gwtypes.HTTPRoute
}

// RootLoader is an interface that defines methods for setting the root object.
type RootLoader[t RootObject] interface {
	SetRootObject(obj t)
}

// StoreLoader is an interface that defines methods for loading the store.
type StoreLoader interface {
	LoadStore(ctx context.Context) error
}

// Translator is an interface that defines methods for translating the loaded store into the desired output.
type Translator interface {
	Translate() error
}
