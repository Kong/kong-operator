package utils

import (
	"testing"
)

func TestHash(t *testing.T) {
	tests := []struct {
		name     string
		obj1     string
		obj2     string
		sameHash bool
	}{
		{
			name:     "DifferentStringsProduceDifferentHashes",
			obj1:     "foo",
			obj2:     "bar",
			sameHash: false,
		},
		{
			name:     "SameStringProducesSameHash",
			obj1:     "foo",
			obj2:     "foo",
			sameHash: true,
		},
		{
			name:     "EmptyString",
			obj1:     "",
			obj2:     "",
			sameHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := Hash(tt.obj1)
			hash2 := Hash(tt.obj2)
			if (hash1 == hash2) != tt.sameHash {
				t.Errorf("Test %s: expected sameHash=%v, got hash1=%s, hash2=%s", tt.name, tt.sameHash, hash1, hash2)
			}
		})
	}
}
