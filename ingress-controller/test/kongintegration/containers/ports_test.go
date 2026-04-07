package containers

import (
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestBindLocalPort(t *testing.T) {
	t.Run("normalizes port and sets host binding", func(t *testing.T) {
		req := testcontainers.ContainerRequest{}

		BindLocalPort(t, &req, nat.Port("8001"))

		require.Equal(t, []string{"8001/tcp"}, req.ExposedPorts)
		require.NotNil(t, req.HostConfigModifier)

		hostConfig := &container.HostConfig{}
		req.HostConfigModifier(hostConfig)

		port, ok := network.PortFrom(8001, network.TCP)
		require.True(t, ok)

		bindings := hostConfig.PortBindings[port]
		require.Len(t, bindings, 1)
		assert.Equal(t, "0.0.0.0", bindings[0].HostIP.String())
		assert.NotEmpty(t, bindings[0].HostPort)
	})

	t.Run("preserves existing host config modifier", func(t *testing.T) {
		req := testcontainers.ContainerRequest{
			HostConfigModifier: func(hostConfig *container.HostConfig) {
				hostConfig.CapAdd = []string{"NET_ADMIN"}
			},
		}

		BindLocalPort(t, &req, nat.Port("8000"))

		hostConfig := &container.HostConfig{}
		req.HostConfigModifier(hostConfig)

		assert.Equal(t, []string{"NET_ADMIN"}, hostConfig.CapAdd)
		port, ok := network.PortFrom(8000, network.TCP)
		require.True(t, ok)
		require.Len(t, hostConfig.PortBindings[port], 1)
	})
}
