package managedfields

import (
	"reflect"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func isNumericKind(kind reflect.Kind) bool {
	return kind == reflect.Int ||
		kind == reflect.Int8 ||
		kind == reflect.Int16 ||
		kind == reflect.Int32 ||
		kind == reflect.Int64 ||
		kind == reflect.Uint ||
		kind == reflect.Uint8 ||
		kind == reflect.Uint16 ||
		kind == reflect.Uint32 ||
		kind == reflect.Uint64 ||
		kind == reflect.Uintptr ||
		kind == reflect.Float32 ||
		kind == reflect.Float64
}

// Recursively remove empty maps and slices from a map[string]interface{}.
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
			for _, v := range slices.Backward(emptyIndices) {
				idx := v
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
			// Don't delete boolean fields even if they're false.
			if rv.Kind() == reflect.Bool {
				continue
			}
			// Don't delete numeric zero values. They can be semantically meaningful,
			// for example KongTarget.spec.weight=0 for Gateway API weighted backends.
			if isNumericKind(rv.Kind()) {
				continue
			}
			// Don't delete pointer fields that point to zero values (user explicitly set them to zero).
			// Only delete if the pointer itself is nil.
			if rv.Kind() == reflect.Pointer {
				if rv.IsNil() {
					delete(m, k)
				}
				continue
			}
			// For non-pointer, non-bool types, delete if zero value.
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
