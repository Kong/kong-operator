// Package containers provides test containers for the AI Gateway contract
// tests, modeled on the Kong test containers in
// ingress-controller/test/kongintegration/containers.
package containers

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	kongcontainers "github.com/kong/kong-operator/v2/ingress-controller/test/kongintegration/containers"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
	"github.com/kong/kong-operator/v2/test"
)

const (
	aigwAdminPort  = "8001"
	aigwProxyPort  = "8000"
	aigwStatusPort = "8100"
)

// MockCustomPolicyName is the streaming custom policy the representative
// document carries. The plugin instance referencing it is only accepted by
// the Admin API when the plugin is enabled on the data plane (KONG_PLUGINS)
// and its code is on disk — the same setup the operator's data plane
// deployments provision for installed and streaming custom policies. The
// contract test's fixtures reference these constants so the payload's
// custom_plugins entry and the container's on-disk plugin cannot drift.
const (
	MockCustomPolicyName = "mock-custom-policy"

	MockCustomPolicyHandlerLua = `local P = { PRIORITY = 1000, VERSION = "1.0.0" }
function P:header_filter(conf) kong.response.set_header("X-Mock-Custom-Policy", "applied") end
return P`

	MockCustomPolicySchemaLua = `return {
  name = "mock-custom-policy",
  fields = {
    { config = {
        type = "record",
        fields = {
          { header_name = { type = "string", default = "X-Mock-Custom-Policy" } },
        },
      } },
  },
}`
)

// AIGateway represents a docker container running Kong AI Gateway.
type AIGateway struct {
	container testcontainers.Container
}

// NewAIGateway spawns a docker container running the AI Gateway image (its
// version is determined by environment variables / test dependencies). It
// registers a cleanup function that terminates the container when the test
// finishes.
//
// The container boots with a plain (non-TLS) Admin API listener: the contract
// under test is the payload the operator pushes, not the transport the
// operator provisions (mTLS) for it.
func NewAIGateway(ctx context.Context, t *testing.T) AIGateway {
	// The request is built fresh on every retry attempt so BindLocalPort picks
	// a new host port each time, mirroring NewKong: GetFreePort only proves a
	// port was free at check time, and BindLocalPort appends to ExposedPorts
	// and chains HostConfigModifier, so reuse would rebind the same doomed port
	// and accumulate stale bindings.
	newRequest := func() testcontainers.ContainerRequest {
		req := testcontainers.ContainerRequest{
			Image: aigwImageUnderTest(t),
			Env: map[string]string{
				"KONG_DATABASE":      "off",
				"KONG_ADMIN_LISTEN":  fmt.Sprintf("0.0.0.0:%s", aigwAdminPort),
				"KONG_PROXY_LISTEN":  fmt.Sprintf("0.0.0.0:%s", aigwProxyPort),
				"KONG_STATUS_LISTEN": fmt.Sprintf("0.0.0.0:%s", aigwStatusPort),
				"KONG_VITALS":        "off",
				"KONG_LICENSE_DATA":  test.KongLicenseData(),
				// The representative document carries a plugin instance of the
				// streaming custom policy; the Admin API only accepts it when the
				// plugin is enabled and its code is loadable (mirroring what the
				// operator provisions on data planes).
				"KONG_PLUGINS": "bundled," + MockCustomPolicyName,
			},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort(aigwAdminPort),
				wait.ForHTTP("/").WithPort(aigwAdminPort),
			),
		}
		kongcontainers.BindLocalPort(t, &req, aigwAdminPort)
		kongcontainers.BindLocalPort(t, &req, aigwProxyPort)
		// The status listener is what serves /status/ready; the Admin API does
		// not. It must be reachable from the host for the readiness contract
		// test.
		kongcontainers.BindLocalPort(t, &req, aigwStatusPort)
		return req
	}

	aigwC, err := retry.NewWithData[testcontainers.Container](
		retry.Context(ctx),
		retry.Attempts(10),
		retry.Delay(200*time.Millisecond),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(_ uint, err error) {
			t.Logf("failed creating AI Gateway container: %v", err)
		}),
	).Do(
		func() (testcontainers.Container, error) {
			c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
				ContainerRequest: newRequest(),
				Started:          false,
			})
			if err != nil {
				return nil, err
			}
			// The custom policy's plugin code must be on the container's Lua
			// path before Kong boots, or KONG_PLUGINS fails at startup.
			pluginDir := "/usr/local/share/lua/5.1/kong/plugins/" + MockCustomPolicyName
			for name, content := range map[string]string{
				"handler.lua": MockCustomPolicyHandlerLua,
				"schema.lua":  MockCustomPolicySchemaLua,
			} {
				if err := c.CopyToContainer(ctx, []byte(content), pluginDir+"/"+name, 0o644); err != nil {
					_ = c.Terminate(context.Background())
					return nil, fmt.Errorf("copying %s into the container: %w", name, err)
				}
			}
			if err := c.Start(ctx); err != nil {
				// A container that never started is not registered with the
				// test's cleanup; terminate it here or it leaks.
				_ = c.Terminate(context.Background())
				return nil, err
			}
			return c, nil
		},
	)
	require.NoError(t, err)

	aigw := AIGateway{
		container: aigwC,
	}
	t.Cleanup(func() { //nolint:contextcheck
		// If the container is already terminated, we don't need to terminate it again.
		if aigwC.IsRunning() {
			assert.NoError(t, aigwC.Terminate(context.Background()))
		}
	})

	return aigw
}

// AdminURL returns the Admin API URL of the AI Gateway container reachable
// from the host machine.
func (a AIGateway) AdminURL(ctx context.Context, t *testing.T) string {
	host, err := a.container.Host(ctx)
	require.NoError(t, err)

	port, err := a.container.MappedPort(ctx, aigwAdminPort)
	require.NoError(t, err)
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port.Port()))
}

// StatusURL returns the URL of the AI Gateway container's status listener,
// which serves the /status/ready endpoint the operator's data plane readiness
// probe hits, reachable from the host machine.
func (a AIGateway) StatusURL(ctx context.Context, t *testing.T) string {
	host, err := a.container.Host(ctx)
	require.NoError(t, err)

	port, err := a.container.MappedPort(ctx, aigwStatusPort)
	require.NoError(t, err)
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port.Port()))
}

// aigwImageUnderTest returns the AI Gateway image to be used for the contract
// tests. If both TEST_AIGW_IMAGE and TEST_AIGW_TAG are set, it returns the
// image and tag they specify. Otherwise it returns kong/kong-ai-gateway-dev with
// the version pinned in test/test_dependencies.yaml.
func aigwImageUnderTest(t *testing.T) string {
	t.Helper()

	image, tag := os.Getenv("TEST_AIGW_IMAGE"), os.Getenv("TEST_AIGW_TAG")
	if image != "" && tag != "" {
		return fmt.Sprintf("%s:%s", image, tag)
	}

	aigwTag, err := testenv.GetDependencyVersion("kongintegration.aigw-dp")
	require.NoError(t, err)
	return "kong/kong-ai-gateway-dev:" + aigwTag
}
