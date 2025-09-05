package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToUnstructured converts a client.Object to an unstructured.Unstructured object.
// Returns the unstructured object and any error encountered during conversion.
func ToUnstructured(obj client.Object) (unstructured.Unstructured, error) {
	var err error
	out := unstructured.Unstructured{}
	out.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	return out, err
}

// FromUnstructured converts an unstructured.Unstructured object to a typed client.Object.
// It returns an error if the conversion fails.
func FromUnstructured(in unstructured.Unstructured, out client.Object) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(in.Object, out)
}
