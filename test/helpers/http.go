package helpers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// MustCreateHTTPClient creates an HTTP client with the given TLS secret and host.
func MustCreateHTTPClient(t *testing.T, tlsSecret *corev1.Secret, host string) *http.Client {
	t.Helper()

	httpClient, err := createHTTPClient(tlsSecret, host)
	require.NoError(t, err)
	return httpClient
}

// CreateHTTPClient creates an HTTP client with the given TLS secret and host and returns an error if it fails.
func CreateHTTPClient(tlsSecret *corev1.Secret, host string) (*http.Client, error) {
	return createHTTPClient(tlsSecret, host)
}

func createHTTPClient(tlsSecret *corev1.Secret, host string) (*http.Client, error) {
	var tlsClientConfig *tls.Config
	var err error
	if tlsSecret != nil {
		tlsClientConfig, err = createTLSClientConfig(tlsSecret, host)
		if err != nil {
			return nil, err
		}
	}
	return &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: tlsClientConfig,
		},
	}, nil
}

func createTLSClientConfig(tlsSecret *corev1.Secret, server string) (*tls.Config, error) {
	certPem, ok := tlsSecret.Data["tls.crt"]
	if !ok {
		return nil, errors.New("tls.crt not found in secret")
	}
	keyPem, ok := tlsSecret.Data["tls.key"]
	if !ok {
		return nil, errors.New("tls.key not found in secret")
	}
	if server == "" {
		return nil, errors.New("server name required for TLS")
	}

	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil, fmt.Errorf("unexpected error creating X509KeyPair: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(certPem) {
		return nil, errors.New("unexpected error adding trusted CA")
	}

	// Disable G402: TLS MinVersion too low. (gosec)
	// #nosec G402
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   server,
		RootCAs:      certPool,
	}, nil
}

// MustBuildRequest creates an HTTP request with the given method, URL, and host.
func MustBuildRequest(t *testing.T, ctx context.Context, method, url, host string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	require.NoError(t, err)
	if host != "" {
		req.Host = host
	}
	return req
}
