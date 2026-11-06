package metricsscraper

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
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
