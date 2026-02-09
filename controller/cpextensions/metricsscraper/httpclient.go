package metricsscraper

import (
	"crypto/tls"
	"crypto/x509"
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
