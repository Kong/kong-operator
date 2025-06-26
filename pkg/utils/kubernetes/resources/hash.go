package resources

import (
	"fmt"

	"github.com/gohugoio/hashstructure"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/pkg/consts"
)

// CalculateHash calculates the hash of the given object.
// It returns the hash as a string.
func CalculateHash[T any](
	obj T,
) (string, error) {
	hash, err := hashstructure.Hash(obj, nil)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%0x", hash), nil
}

// SpecHashMatchesAnnotation calculates the hash of the given spec and returns boolean
// indicating whether the hash matches the one in the annotations of the given
// object.
func SpecHashMatchesAnnotation[T any](
	spec T,
	obj client.Object,
) (bool, error) {
	hash, err := CalculateHash(spec)
	if err != nil {
		return false, fmt.Errorf("failed to calculate hash spec from %T: %w", spec, err)
	}
	if h, ok := obj.GetAnnotations()[consts.AnnotationSpecHash]; !ok || h != hash {
		return false, nil
	}
	return true, nil
}
