package generator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortModuleCacheMatchesUsesSemver(t *testing.T) {
	t.Parallel()

	matches := []string{
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.6.0",
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.36.0",
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.12.0",
	}

	sortModuleCacheMatches(matches)

	require.Equal(t, []string{
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.6.0",
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.12.0",
		"/tmp/mod/github.com/!kong/sdk-konnect-go@v0.36.0",
	}, matches)
}
