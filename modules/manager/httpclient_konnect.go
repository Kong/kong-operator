package manager

import (
	"net/http"
	"time"

	"github.com/hashicorp/go-cleanhttp"
)

func httpClientForKonnect(c *Config) *http.Client {
	transport := cleanhttp.DefaultPooledTransport()
	transport.IdleConnTimeout = 2 * c.KonnectSyncPeriod
	// Configure HTTP/2 health checks to detect stalled connections.
	// When no frames are received for SendPingTimeout, a PING is sent.
	// If no PING response is received within PingTimeout, the connection is closed
	// and removed from the pool, forcing new requests to open a fresh connection.
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.KonnectRequestTimeout,
	}
}
