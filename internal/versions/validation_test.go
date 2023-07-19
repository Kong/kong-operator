package versions

import (
	"testing"

	"github.com/kong/semver/v4"
	"github.com/stretchr/testify/require"

	kgoerrors "github.com/kong/gateway-operator/internal/errors"
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
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.Tag, func(t *testing.T) {
			actual, err := FromImage(tc.Tag)

			if tc.ExpectedError != nil {
				require.Error(t, err)
				require.ErrorAs(t, err, &tc.ExpectedError)
			} else {
				require.NoError(t, err)
				expected := tc.Expected(t)
				require.Equal(t, expected, actual)
			}
		})
	}
}
