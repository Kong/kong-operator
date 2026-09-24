/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package adminapi

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	"github.com/kong/go-kong/kong"

	"github.com/kong/kong-operator/v2/modules/manager/metadata"
)

// NewMTLSClient returns a Kong Admin API client for the given address, verifying
// the server certificate against the provided CA certificate (PEM) using serverName
// as the TLS SNI, and authenticating with the provided client certificate and key
// (PEM). It is used to push configuration to Admin APIs secured with mTLS, e.g. the
// Admin API of the AIGatewayDataPlanes referenced by an OnPremAIGateway.
func NewMTLSClient(address, serverName string, certPEM, keyPEM, caPEM []byte) (*kong.Client, error) {
	tlsConfig := tls.Config{
		ServerName: serverName,
	}

	if len(caPEM) > 0 {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to load CA certificate")
		}
		tlsConfig.RootCAs = certPool
	}

	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}
	tlsConfig.Certificates = []tls.Certificate{clientCert}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tlsConfig

	client, err := kong.NewClient(&address, &http.Client{Transport: transport})
	if err != nil {
		return nil, fmt.Errorf("creating Kong client: %w", err)
	}
	client.UserAgent = metadata.Metadata().UserAgent()
	return client, nil
}
