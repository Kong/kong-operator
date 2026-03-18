package manager

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"golang.org/x/net/http2"
)

func httpClientForKonnect(c *Config) (*http.Client, error) {
	// Clone the default transport to inherit all Go defaults (TLS settings,
	// timeouts, proxy support, etc.) and only override what we need.
	transport := cleanhttp.DefaultPooledTransport()
	transport.IdleConnTimeout = 2 * c.KonnectSyncPeriod

	// Configure HTTP/2 health checks to detect stalled connections.
	// When no frames are received for ReadIdleTimeout, a PING is sent.
	// If no PING response within PingTimeout, the connection is closed
	// and removed from the pool, forcing new requests to open a fresh connection.
	h2Transport, err := http2.ConfigureTransports(transport)
	if err != nil {
		return nil, fmt.Errorf("failed to configure HTTP/2 transport: %w", err)
	}
	h2Transport.ReadIdleTimeout = 30 * time.Second
	h2Transport.PingTimeout = 15 * time.Second

	return &http.Client{
		Transport: transport,
		Timeout:   c.KonnectRequestTimeout,
	}, nil
}
