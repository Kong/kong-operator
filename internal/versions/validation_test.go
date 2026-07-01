package versions

import (
	"testing"

	"github.com/kong/semver/v4"
	"github.com/stretchr/testify/require"

	kgoerrors "github.com/kong/kong-operator/v2/internal/errors"
)

func Test_versionFromImage(t *testing.T) {
	testcases := []struct {
		Tag           string
		Expected      func(t *testing.T) semver.Version
		ExpectedError error
	}{
		{
			Tag: "kong/kong-gateway:3.3.0.0",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3.0",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3.0.0-alpine",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3.0-alpine",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3.0-rhel",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3-rhel",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag:           "kong/kong-gateway:3a.3.y.y",
			ExpectedError: kgoerrors.ErrInvalidSemverVersion,
		},
		{
			Tag:           "kong/kong-gateway:3.a3.y.y",
			ExpectedError: kgoerrors.ErrInvalidSemverVersion,
		},
		{
			Tag: "kong/kong-gateway:3.14",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.14.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.14@sha256:8f0089833902c555bf02dee1a59d3fe1f9fed11745eb7dd75e9bf844755147c9",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.14.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3.0.0@sha256:abc123def456",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "kong/kong-gateway:3.3-alpine@sha256:abc123def456",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		// Regression: image references whose registry host carries a port
		// number contain more than one ':', but the tag is always after the
		// last one. See https://github.com/Kong/kong-operator/issues/... .
		{
			Tag: "registry.example.com:5000/kong/kong-gateway:3.3.0",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "registry.example.com:5000/kong/kong-gateway:3.10",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.10.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			Tag: "registry.example.com:5000/kong/kong-gateway:3.3-alpine@sha256:abc123def456",
			Expected: func(t *testing.T) semver.Version {
				v, err := semver.Parse("3.3.0")
				require.NoError(t, err)
				return v
			},
		},
		{
			// Registry with port but no tag: the last ':' is inside the host,
			// what follows it contains '/'. Must be rejected as "no tag".
			Tag:           "registry.example.com:5000/kong/kong-gateway",
			ExpectedError: ErrExpectedSemverVersion,
		},
		{
			// No colon at all — no tag can exist.
			Tag:           "kong/kong-gateway",
			ExpectedError: ErrExpectedSemverVersion,
		},
		{
			// Trailing colon with empty tag — invalid.
			Tag:           "kong/kong-gateway:",
			ExpectedError: ErrExpectedSemverVersion,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.Tag, func(t *testing.T) {
			actual, err := FromImage(tc.Tag)
			if tc.ExpectedError != nil {
				require.ErrorIs(t, err, tc.ExpectedError)
			} else {
				require.NoError(t, err)
				expected := tc.Expected(t)
				require.Equal(t, expected, actual)
			}
		})
	}
}
