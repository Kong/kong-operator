package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ToUnstructured converts a client.Object to an unstructured.Unstructured object.
// It infers TypeMeta using the provided scheme if not set.
// Returns the unstructured object and any error encountered during conversion.
func ToUnstructured(obj client.Object, scheme *runtime.Scheme) (unstructured.Unstructured, error) {
	var err error
	out := unstructured.Unstructured{}
	out.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return out, err
	}

	// Infer GVK from scheme if not set
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() && scheme != nil {
		gvks, _, err := scheme.ObjectKinds(obj)
		if err != nil || len(gvks) == 0 {
			return out, err
		}
		gvk = gvks[0]
	}

	out.SetAPIVersion(gvk.GroupVersion().String())
	out.SetKind(gvk.Kind)

	return out, nil
}

// FromUnstructured converts an unstructured.Unstructured object to a typed client.Object.
// It returns an error if the conversion fails.
func FromUnstructured(in unstructured.Unstructured, out client.Object) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(in.Object, out)
}
