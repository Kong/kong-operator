package v1alpha1

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSDKUnionDiscriminatorKeys pins the generator's snake_case forms of the
// discriminators to camelToSnakeCase, which renames the keys to the SDK's.
func TestSDKUnionDiscriminatorKeys(t *testing.T) {
	for _, key := range sdkUnionDiscriminatorKeys {
		assert.True(t, slices.Contains(sdkUnionDiscriminatorKeys, camelToSnakeCase(key)),
			"%q renames to %q, which sdkUnionDiscriminatorKeys lacks", key, camelToSnakeCase(key))
	}
}
