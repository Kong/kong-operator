package metricsscraper

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"sync"
	"time"
)

func httpClientWithCerts(certs certs) *http.Client {
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(certs.CA)

	httpClient := *http.DefaultClient
	httpClient.Timeout = 10 * time.Second
	httpClient.Transport = &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{
				{
					Certificate: [][]byte{
						certs.Cert.Raw,
					},
					Leaf:       certs.Cert,
					PrivateKey: certs.Key,
				},
			},
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		},
	}
	return &httpClient
}

// endpointHTTPClients caches the per-endpoint HTTP clients. Each cloned
// transport owns its own connection pool, so cloning on every call would pay a
// new TCP and TLS handshake every time and leak the dropped pool's connections.
type endpointHTTPClients struct {
	lock    sync.Mutex
	clients map[string]*http.Client
}

// ForEndpoint returns the cached client for serverName. It clones base on the
// first call for that server name.
func (c *endpointHTTPClients) ForEndpoint(base *http.Client, serverName string) (*http.Client, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if client, ok := c.clients[serverName]; ok {
		return client, nil
	}

	client, err := httpClientForEndpoint(base, serverName)
	if err != nil {
		return nil, err
	}
	if c.clients == nil {
		c.clients = make(map[string]*http.Client)
	}
	c.clients[serverName] = client
	return client, nil
}

// httpClientForEndpoint clones the base client and its transport, then sets the
// TLS server name used to verify the Admin API certificate. It leaves the base
// client untouched so concurrent scrapes do not share divergent TLS settings.
func httpClientForEndpoint(base *http.Client, serverName string) (*http.Client, error) {
	if base == nil {
		base = http.DefaultClient
	}

	httpClient := *base
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unsupported HTTP transport type %T", transport)
	}

	// Transport.Clone already deep-copies TLSClientConfig when it is set, so the
	// returned config is private to this client.
	clonedTransport := httpTransport.Clone()
	tlsConfig := clonedTransport.TLSClientConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	}
	tlsConfig.ServerName = serverName
	clonedTransport.TLSClientConfig = tlsConfig
	httpClient.Transport = clonedTransport
	return &httpClient, nil
}
