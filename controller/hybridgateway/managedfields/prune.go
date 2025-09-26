package managedfields

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Recursively remove empty maps and slices from a map[string]interface{}
func pruneEmptyFields(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			if len(val) == 0 {
				delete(m, k)
			} else {
				pruneEmptyFields(val)
			}
		case []any:
			for i := range val {
				if subMap, ok := val[i].(map[string]any); ok {
					pruneEmptyFields(subMap)
				}
			}
			// Remove empty slices
			if len(val) == 0 {
				delete(m, k)
			}
		default:
			rv := reflect.ValueOf(v)
			if !rv.IsValid() || rv.IsZero() {
				delete(m, k)
			}
		}
	}
}

// PruneEmptyFields removes empty maps, slices, and zero-value fields from the provided unstructured.Unstructured object.
func PruneEmptyFields(u *unstructured.Unstructured) {
	pruneEmptyFields(u.Object)
}
