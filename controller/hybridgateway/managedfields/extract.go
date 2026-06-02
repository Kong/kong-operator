package managedfields

import (
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ExtractAsUnstructured extracts the managed fields for a given field manager and subresource from a runtime.Object,
// returning an unstructured.Unstructured containing only the fields managed by that manager.
// Returns nil if no managed fields entry is found, or an error if extraction fails.
func ExtractAsUnstructured(obj runtime.Object, fieldManager string, subresource string) (*unstructured.Unstructured, error) {
	objectType, err := GetObjectType(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to get object type for managed fields extraction: %w", err)
	}
	typedObj, err := toTyped(obj, objectType)
	if err != nil {
		return nil, fmt.Errorf("error converting obj to typed: %w", err)
	}

	accessor, err := apimeta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("error accessing metadata: %w", err)
	}
	fieldsEntry, ok := k8sutils.FindManagedFieldsEntry(accessor, fieldManager, subresource)
	if !ok {
		return nil, nil
	}
	fieldset := &fieldpath.Set{}
	err = fieldset.FromJSON(fieldsEntry.FieldsV1.GetRawReader())
	if err != nil {
		return nil, fmt.Errorf("error marshalling FieldsV1 to JSON: %w", err)
	}

	u := typedObj.ExtractItems(fieldset.Leaves()).AsValue().Unstructured()
	m, ok := u.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unable to convert managed fields for %s to unstructured, expected map, got %T", fieldManager, u)
	}

	// We set the same gvk for the object that holds the managed fields.
	// We are sure that the gvk is set otherwise the function will error in the
	// previous steps.
	gvk := obj.GetObjectKind().GroupVersionKind()
	m["apiVersion"] = gvk.GroupVersion().String()
	m["kind"] = gvk.Kind

	return &unstructured.Unstructured{
		Object: m,
	}, nil
}

// toTyped converts a runtime.Object to a *typed.TypedValue using the provided ParseableType.
// Handles both unstructured and structured objects.
func toTyped(obj runtime.Object, objectType typed.ParseableType) (*typed.TypedValue, error) {
	switch o := obj.(type) {
	case *unstructured.Unstructured:
		return objectType.FromUnstructured(o.Object)
	default:
		return objectType.FromStructured(o)
	}
}
