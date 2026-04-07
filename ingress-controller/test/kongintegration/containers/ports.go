package containers

import (
	"net/netip"
	"strconv"
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/kong/kong-operator/v2/ingress-controller/test/helpers"
)

// BindLocalPort exposes a container port and binds it to a fixed IPv4 host port.
//
// This preserves the explicit IPv4 binding workaround for moby/moby#42442 without relying on
// Docker accepting host mapping syntax in ContainerRequest.ExposedPorts.
func BindLocalPort(t *testing.T, req *testcontainers.ContainerRequest, containerPort nat.Port) {
	t.Helper()

	normalizedPort, err := nat.NewPort(containerPort.Proto(), containerPort.Port())
	require.NoError(t, err)

	hostPort := strconv.Itoa(helpers.GetFreePort(t))
	req.ExposedPorts = append(req.ExposedPorts, string(normalizedPort))

	previousModifier := req.HostConfigModifier
	req.HostConfigModifier = func(hostConfig *container.HostConfig) {
		if previousModifier != nil {
			previousModifier(hostConfig)
		}
		if hostConfig.PortBindings == nil {
			hostConfig.PortBindings = network.PortMap{}
		}

		port, ok := network.PortFrom(uint16(normalizedPort.Int()), network.IPProtocol(normalizedPort.Proto()))
		require.True(t, ok)
		addr, err := netip.ParseAddr("0.0.0.0")
		require.NoError(t, err)
		hostConfig.PortBindings[port] = append(hostConfig.PortBindings[port], network.PortBinding{
			HostIP:   addr,
			HostPort: hostPort,
		})
	}
}
