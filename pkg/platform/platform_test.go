package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	for _, valid := range []string{"auto", "openshift", "kubernetes"} {
		p, err := New(valid)
		require.NoError(t, err)
		require.Equal(t, Platform(valid), p)
		require.Equal(t, valid, p.String())
	}

	_, err := New("garden-linux")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid platform")
}
