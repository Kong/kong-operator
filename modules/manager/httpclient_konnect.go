package manager

import (
	"net"
	"net/http"
	"runtime"
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

// defaultLongPolledTransport returns a new [http.Transport] with similar default
// values to [http.DefaultTransport] but with idle connections.
// It is intended for use with long-polling requests.
func defaultLongPolledTransport() *http.Transport {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		IdleConnTimeout:     24 * time.Hour,
		// single Konnect host; avoid a 2-conn cap under low GOMAXPROCS
		MaxIdleConnsPerHost: max(runtime.GOMAXPROCS(0)+1, 16),
	}
	// Configure HTTP/2 health checks to detect stalled long-poll connections.
	// The long-poll client has no client-level Timeout, so without this a
	// connection wedged behind a dead L7 intermediary would never recover.
	// A PING is answered by the peer's HTTP/2 layer independent of the held-open
	// long-poll stream, so if it goes unanswered within PingTimeout the connection
	// is closed and the in-flight poll errors out and retries with backoff.
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: 60 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	return transport
}

func httpClientForKonnectLongPolling() *http.Client {
	transport := defaultLongPolledTransport()
	return &http.Client{
		Transport: transport,
	}
}
