package helpers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDockerRegistryConfigManager(t *testing.T) {
	tests := []struct {
		name          string
		update        func(*testing.T, *DockerRegistryConfigManager)
		expectedJSON  string
		expectedError error
	}{
		{
			name:         "Empty",
			expectedJSON: `{"auths":{}}`,
		},
		{
			name: "Valid configuration",
			update: func(t *testing.T, c *DockerRegistryConfigManager) {
				require.NoError(t,
					c.Add(
						"registry1",
						"username1",
						"token1",
					),
				)
			},
			expectedJSON: `{"auths":{"registry1":{"auth":"dXNlcm5hbWUxOnRva2VuMQ=="}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewDockerRegistryConfigManager()
			if tt.update != nil {
				tt.update(t, mgr)
			}

			jsonData, err := mgr.EncodeForRegcred()
			if tt.expectedError != nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedJSON, string(jsonData))
		})
	}
}
