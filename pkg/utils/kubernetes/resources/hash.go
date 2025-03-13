package resources

import (
	"fmt"

	"github.com/gohugoio/hashstructure"
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
