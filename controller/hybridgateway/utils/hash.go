package utils

import (
	"fmt"
	"hash/fnv"

	"k8s.io/kubernetes/pkg/util/hash"
)

// Hash64 returns a hash string representation of the given object using FNV-1a hashing.
func Hash64(obj any) string {
	hasher := fnv.New64a()
	hash.DeepHashObject(hasher, obj)
	hashValue := fmt.Sprintf("%x", hasher.Sum64())
	return hashValue
}

// Hash32 returns a hash string representation of the given object using FNV-1a hashing.
func Hash32(obj any) string {
	hasher := fnv.New32a()
	hash.DeepHashObject(hasher, obj)
	hashValue := fmt.Sprintf("%x", hasher.Sum32())
	return hashValue
}
