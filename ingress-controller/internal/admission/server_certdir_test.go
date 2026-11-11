package admission

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// writeCert writes a self-signed certificate and key into dir and returns the
// certificate in PEM format.
func writeCert(t *testing.T, dir string) []byte {
	t.Helper()

	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("webhook-test"),
		certificate.WithDNSNames("localhost"),
		certificate.WithIPAdresses("127.0.0.1"),
		certificate.WithCATrue(),
	)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), certPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.key"), keyPEM, 0o600))
	return certPEM
}

// dialTLS performs a TLS handshake against addr, trusting only caPEM.
func dialTLS(addr string, caPEM []byte) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return os.ErrInvalid
	}
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: time.Second},
		"tcp", addr,
		&tls.Config{RootCAs: roots, ServerName: "localhost"},
	)
	if err != nil {
		return err
	}
	return conn.Close()
}

func TestMakeTLSServerReadsCertFromConfiguredDir(t *testing.T) {
	handler := http.NewServeMux()

	t.Run("empty directory is rejected", func(t *testing.T) {
		_, err := MakeTLSServer(0, handler, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("directory without certificate is rejected", func(t *testing.T) {
		_, err := MakeTLSServer(0, handler, t.TempDir())
		require.Error(t, err)
	})

	t.Run("server serves the certificate from the configured directory", func(t *testing.T) {
		dir := t.TempDir()
		caPEM := writeCert(t, dir)

		// Reserve a free port for the server.
		ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:gosec // test only
		require.NoError(t, err)
		port := ln.Addr().(*net.TCPAddr).Port
		require.NoError(t, ln.Close())

		srv, err := MakeTLSServer(int32(port), handler, dir)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = srv.Start(ctx) }()

		addr := net.JoinHostPort("localhost", strconv.Itoa(port))
		require.Eventually(t, func() bool {
			return dialTLS(addr, caPEM) == nil
		}, 5*time.Second, 100*time.Millisecond)
	})
}
