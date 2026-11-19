package versions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDataPlaneImageKongConfWatchCapable(t *testing.T) {
	testCases := []struct {
		name     string
		image    string
		expected bool
	}{
		{
			name:     "the first release carrying the watcher",
			image:    "kong/kong-gateway:3.17.0.0",
			expected: true,
		},
		{
			name:     "a later release",
			image:    "kong/kong-gateway:3.18.0.0",
			expected: true,
		},
		{
			name:     "a flavoured tag",
			image:    "kong/kong-gateway:3.17.0.0-ubuntu",
			expected: true,
		},
		{
			name:     "a release candidate of the first release",
			image:    "kong/kong-gateway:3.17.0.0-rc.1",
			expected: true,
		},
		{
			name:     "the release before it",
			image:    "kong/kong-gateway:3.16.0.0",
			expected: false,
		},
		{
			name:     "an older release",
			image:    "kong/kong-gateway:3.10",
			expected: false,
		},
		{
			// The Gateway team publishes per-PR development images for exactly this
			// kind of testing, and they carry no semver tag.
			name:     "a development image with an unparseable tag",
			image:    "kong/kong-gateway-dev:pr-23439-ubuntu",
			expected: true,
		},
		{
			name:     "an image with no tag at all",
			image:    "kong/kong-gateway",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsDataPlaneImageKongConfWatchCapable(tc.image))
		})
	}
}
