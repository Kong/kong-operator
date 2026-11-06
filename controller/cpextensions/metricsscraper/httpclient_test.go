package metricsscraper

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type unsupportedTransport struct{}

func (unsupportedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unused")
}

func TestHTTPClientForEndpoint(t *testing.T) {
	base := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName: "base.example.com",
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	first, err := httpClientForEndpoint(base, "pod.first.default.svc")
	require.NoError(t, err)
	second, err := httpClientForEndpoint(base, "pod.second.default.svc")
	require.NoError(t, err)

	require.NotSame(t, first, second)
	require.NotSame(t, base.Transport, first.Transport)
	require.Equal(t, "pod.first.default.svc", first.Transport.(*http.Transport).TLSClientConfig.ServerName)
	require.Equal(t, "pod.second.default.svc", second.Transport.(*http.Transport).TLSClientConfig.ServerName)
	require.Equal(t, "base.example.com", base.Transport.(*http.Transport).TLSClientConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), first.Transport.(*http.Transport).TLSClientConfig.MinVersion)
}

func TestHTTPClientForEndpointRejectsCustomTransport(t *testing.T) {
	base := &http.Client{Transport: unsupportedTransport{}}

	_, err := httpClientForEndpoint(base, "pod.default.svc")
	require.ErrorContains(t, err, "unsupported HTTP transport type")
}
