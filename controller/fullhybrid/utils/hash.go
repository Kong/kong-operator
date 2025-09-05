package utils

import (
	"fmt"
	"hash/fnv"

	"k8s.io/kubernetes/pkg/util/hash"
)

// Hash returns a hash string representation of the given object using FNV-1a hashing.
func Hash(obj any) string {
	hasher := fnv.New64a()
	hash.DeepHashObject(hasher, obj)
	hashValue := fmt.Sprintf("%x", hasher.Sum64())
	return hashValue
}
