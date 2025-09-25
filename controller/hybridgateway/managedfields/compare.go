package managedfields

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

// Compare calculates the difference between the current and desired objects.
func Compare(current, desired *unstructured.Unstructured) (*typed.Comparison, error) {
	// Extract managed fields for our field manager using structured-merge-diff.
	currentParsable, err := GetObjectType(current)
	if err != nil {
		return nil, err
	}

	desiredParsable, err := GetObjectType(desired)
	if err != nil {
		return nil, err
	}

	// Parse the base resource
	currentTyped, err := currentParsable.FromUnstructured(current.Object)
	if err != nil {
		return nil, err
	}

	// Parse the user defined resource
	desiredTyped, err := desiredParsable.FromUnstructured(desired.Object)
	if err != nil {
		return nil, err
	}

	return currentTyped.Compare(desiredTyped)
}
