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
				// Check if the map became empty after pruning.
				if len(val) == 0 {
					delete(m, k)
				}
			}
		case []any:
			// First pass: prune all maps in the slice.
			for i := range val {
				if subMap, ok := val[i].(map[string]any); ok {
					pruneEmptyFields(subMap)
				}
			}

			// Second pass: collect indices of empty maps to remove.
			var emptyIndices []int
			for i := range val {
				if subMap, ok := val[i].(map[string]any); ok && len(subMap) == 0 {
					emptyIndices = append(emptyIndices, i)
				}
			}

			// Remove empty maps from slice (in reverse order to maintain correct indices).
			for i := len(emptyIndices) - 1; i >= 0; i-- {
				idx := emptyIndices[i]
				val = append(val[:idx], val[idx+1:]...)
			}

			// Update the slice in the map if we removed items.
			if len(emptyIndices) > 0 {
				m[k] = val
			}

			// Remove empty slices.
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
