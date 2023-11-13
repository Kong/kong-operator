package versions

import (
	"testing"

	"github.com/kong/semver/v4"
	"github.com/stretchr/testify/require"
)

func TestDefaultControlPlaneVersion(t *testing.T) {
	_, err := semver.Parse(DefaultControlPlaneVersion)
	require.NoError(t, err)
}
